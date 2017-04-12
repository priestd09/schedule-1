package main

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// data types for parsing and processing input data

type Instructor struct {
	Name    string
	Times   map[*Time]Badness
	Courses []*Course
}

type Course struct {
	Name       string
	Instructor *Instructor
	Rooms      map[*Room]Badness
	Times      map[*Time]Badness
	Conflicts  map[*Course]Badness
	TwoSlots   bool
	PinRoom    *Room
	PinTime    *Time
}

type Room struct {
	Name     string
	Tags     []string
	Position int
}

type Time struct {
	Name     string
	Tags     []string
	Next     *Time
	Position int
}

type Badness struct {
	N       int
	Message string
}

var impossible = Badness{N: -1, Message: ""}

type Conflict struct {
	Badness Badness
	Courses []*Course
}

type DataSet struct {
	Instructors map[string]*Instructor
	Rooms       map[string]*Room
	Times       map[string]*Time
	TagToRooms  map[string][]*Room
	TagToTimes  map[string][]*Time
	Conflicts   []Conflict
}

// data types to represent a search in progress
type RoomTimeBadness struct {
	Room    *Room
	Time    *Time
	Badness Badness
}

type Section struct {
	Instructor      *Instructor
	Course          *Course
	RoomTimeOptions []RoomTimeBadness
}

type InstructorTime struct {
	Instructor *Instructor
	Time       *Time
}

type CourseTime struct {
	Course *Course
	Time   *Time
}

type RoomTime struct {
	Room *Room
	Time *Time
}

type SearchState struct {
	Data                  *DataSet
	Sections              []*Section
	InstructorTimeBadness map[InstructorTime]Badness
	CourseTimeBadness     map[CourseTime]Badness
	RoomTimeBadness       map[RoomTime]Badness
}

type CoursePlacement struct {
	Course  *Course
	Room    *Room
	Time    *Time
	Badness Badness
}

type SearchResult struct {
	Badness  int
	Schedule []*CoursePlacement
}

func NewSearchState(data *DataSet) *SearchState {
	state := &SearchState{
		Data: data,
		InstructorTimeBadness: make(map[InstructorTime]Badness),
		CourseTimeBadness:     make(map[CourseTime]Badness),
		RoomTimeBadness:       make(map[RoomTime]Badness),
	}

	// fill in RoomTimeBadness
	for _, room := range data.Rooms {
		for _, time := range data.Times {
			state.RoomTimeBadness[RoomTime{room, time}] = Badness{0, ""}
		}
	}

	// fill in InstructorTimeBadness
	for _, instructor := range data.Instructors {
		// start with impossible then correct it for available slots
		for _, time := range data.Times {
			state.InstructorTimeBadness[InstructorTime{instructor, time}] = impossible
		}
		for time, badness := range instructor.Times {
			state.InstructorTimeBadness[InstructorTime{instructor, time}] = badness
		}

		// fill in CourseTimeBadness
		// and prepare RoomTimeBadness list for the Section
		for _, course := range instructor.Courses {
			// start with impossible, correct it for available slots later (see below)
			for _, time := range data.Times {
				state.CourseTimeBadness[CourseTime{course, time}] = impossible
			}

			// record available room/time pairs for this course
			var roomTimeOptions []RoomTimeBadness
			for room, roomBadness := range course.Rooms {
				// is this course pinned to a room?
				if course.PinRoom != nil && course.PinRoom != room {
					continue
				}

				// intersect course times with instructor times
				for time, instructorTimeBadness := range instructor.Times {
					// is this course pinned to a time?
					if course.PinTime != nil && course.PinTime != time {
						continue
					}

					courseTimeBadness, present := course.Times[time]
					if !present {
						courseTimeBadness = impossible
					}

					// if no course times specified, just use instructor times
					if len(course.Times) == 0 {
						courseTimeBadness = instructorTimeBadness
					}

					// if course requires two time slots, make sure this time has a
					// following slot
					if course.TwoSlots && time.Next == nil {
						courseTimeBadness = impossible
					}

					badness := worst(roomBadness, courseTimeBadness, instructorTimeBadness)
					if badness.N < 0 {
						continue
					}

					// note available times in CourseTimeBadness
					state.CourseTimeBadness[CourseTime{course, time}] = badness

					// make an entry for the section
					rtb := RoomTimeBadness{
						Room:    room,
						Time:    time,
						Badness: badness,
					}
					roomTimeOptions = append(roomTimeOptions, rtb)
				}
			}

			if len(roomTimeOptions) == 0 {
				log.Printf("after intersecting available times for instructor %q", instructor.Name)
				log.Printf("and course %q, no valid times are left", course.Name)
				log.Printf("this schedule is doomed to fail")
			}

			// create the section
			section := &Section{
				Instructor:      instructor,
				Course:          course,
				RoomTimeOptions: roomTimeOptions,
			}
			state.Sections = append(state.Sections, section)
		}
	}

	return state
}

func (state *SearchState) Clone() *SearchState {
	new := &SearchState{
		Data: state.Data,
		InstructorTimeBadness: make(map[InstructorTime]Badness),
		CourseTimeBadness:     make(map[CourseTime]Badness),
		RoomTimeBadness:       make(map[RoomTime]Badness),
	}

	for _, elt := range state.Sections {
		new.Sections = append(new.Sections, elt)
	}
	for k, v := range state.InstructorTimeBadness {
		new.InstructorTimeBadness[k] = v
	}
	for k, v := range state.CourseTimeBadness {
		new.CourseTimeBadness[k] = v
	}
	for k, v := range state.RoomTimeBadness {
		new.RoomTimeBadness[k] = v
	}
	return new
}

func worst(lst ...Badness) Badness {
	bad := impossible
	for i, n := range lst {
		if n.N < 0 || n.N >= 100 {
			return impossible
		}
		if i == 0 || n.N > bad.N {
			bad = n
		}
	}
	return bad
}

func (state *SearchState) CollectRoomTimeOptions(section *Section) []RoomTimeBadness {
	var lst []RoomTimeBadness
	for _, rtb := range section.RoomTimeOptions {
		if badness := state.RoomTimeBadness[RoomTime{rtb.Room, rtb.Time}]; badness.N < 0 {
			continue
		}
		if section.Course.TwoSlots {
			if badness := state.RoomTimeBadness[RoomTime{rtb.Room, rtb.Time.Next}]; badness.N < 0 {
				continue
			}
		}

		instructorBadness := state.InstructorTimeBadness[InstructorTime{section.Instructor, rtb.Time}]
		courseBadness := state.CourseTimeBadness[CourseTime{section.Course, rtb.Time}]
		if badness := worst(rtb.Badness, instructorBadness, courseBadness); badness.N >= 0 {
			lst = append(lst, RoomTimeBadness{
				Room:    rtb.Room,
				Time:    rtb.Time,
				Badness: badness,
			})
		}
	}
	return lst
}

func (state *SearchState) SortSections(sections []*Section) {
	options := make(map[*Section]int)
	for _, section := range sections {
		options[section] = len(state.CollectRoomTimeOptions(section))
	}
	sort.Slice(sections, func(a, b int) bool {
		return options[sections[a]] < options[sections[b]]
	})
}

func (state *SearchState) PrintSections() {
	state.SortSections(state.Sections)
	for _, section := range state.Sections {
		var lst []string
		for _, rtb := range state.CollectRoomTimeOptions(section) {
			s := fmt.Sprintf("%s:%s", rtb.Room.Name, rtb.Time.Name)
			lst = append(lst, s)
		}
		sort.Strings(lst)
		fmt.Printf("%s => %s @ [%s]\n",
			section.Instructor.Name,
			section.Course.Name,
			strings.Join(lst, " "))
	}
}

func main() {
	rand.Seed(time.Now().UnixNano())
	log.SetFlags(log.Ltime)

	attempts := 100000
	if len(os.Args) == 2 {
		n, err := strconv.Atoi(os.Args[1])
		if err != nil {
			log.Fatalf("Usage: %s [attempts]", os.Args[0])
		}
		if n < 1 {
			log.Fatalf("minimum of 1 attempts")
		}
		attempts = n
	}

	data := &DataSet{
		Instructors: make(map[string]*Instructor),
		Rooms:       make(map[string]*Room),
		Times:       make(map[string]*Time),

		TagToRooms: make(map[string][]*Room),
		TagToTimes: make(map[string][]*Time),
	}

	// parse everything from CSV
	filename := "input.csv"
	log.Printf("parsing input file %s", filename)
	if err := data.Parse(filename); err != nil {
		log.Fatalf("%v", err)
	}

	s := ""
	if attempts != 1 {
		s = "s"
	}
	log.Printf("beginning %d attempt%s", attempts, s)
	var wg sync.WaitGroup
	pristine := NewSearchState(data)
	workers := runtime.NumCPU()
	start := time.Now()

	// one goroutine assigns jobs
	keepGoing := make(chan struct{}, workers)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < attempts; i++ {
			keepGoing <- struct{}{}
		}
		close(keepGoing)
	}()

	// one goroutine gathers results
	results := make(chan *SearchResult, workers)
	wg.Add(1)
	go func() {
		defer wg.Done()

		total, count := 0, 0
		var best *SearchResult
		lastReport := start
		for i := 0; i < attempts; i++ {
			result := <-results

			if result.Badness < 0 {
				continue
			}

			if count == 0 || result.Badness < best.Badness {
				log.Printf("new best score with badness %d", result.Badness)
				best = result
				fp, err := os.Create("schedule.html")
				if err != nil {
					log.Fatalf("%v", err)
				}
				writeRoomByTime(fp, result)
				fp.Close()

				fp, err = os.Create("schedule.csv")
				if err != nil {
					log.Fatalf("%v", err)
				}
				writeCSV(fp, data, result)
				fp.Close()
			} else if time.Since(lastReport) > time.Minute {
				log.Printf("so far: %d runs in %v, best score of %d", i, round(time.Since(start), time.Second), best.Badness)
				lastReport = time.Now()
			}

			total += result.Badness
			count++
		}

		if count > 0 {
			log.Printf("best score was %d, average was %d", best.Badness, total/count)
		}
		log.Printf("%d successful runs out of %d attempts in %v", count, attempts, round(time.Since(start), time.Second))
		log.Printf("average time per attempt: %v", round(time.Since(start)/time.Duration(attempts), time.Microsecond))
	}()

	// other goroutines run jobs
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for range keepGoing {
				state := pristine.Clone()
				result := new(SearchResult)
				state.Solve(0, result)
				Complain(state.Data, result)
				results <- result
			}
		}()
	}

	wg.Wait()

}

func (state *SearchState) Solve(head int, result *SearchResult) {
	if head == len(state.Sections) {
		// success
		return
	}

	// sort remaining sections by number of options
	if head%15 == 0 {
		state.SortSections(state.Sections[head:])
	}

	// pick out the next section to place
	section := state.Sections[head]

	// pick an assignment for the first section in the list
	options := state.CollectRoomTimeOptions(section)
	if len(options) == 0 {
		// failure
		result.Badness = -1
		return
	}

	// run a lottery to pick the next choice
	tickets := 0
	for _, elt := range options {
		tickets += 100 - elt.Badness.N
	}
	winner := rand.Intn(tickets)
	var rtb RoomTimeBadness
	for _, elt := range options {
		winner -= 100 - elt.Badness.N
		if winner < 0 {
			rtb = elt
			break
		}
	}

	// place the next section
	state.RoomTimeBadness[RoomTime{rtb.Room, rtb.Time}] = impossible
	state.InstructorTimeBadness[InstructorTime{section.Instructor, rtb.Time}] = impossible
	if section.Course.TwoSlots {
		state.RoomTimeBadness[RoomTime{rtb.Room, rtb.Time.Next}] = impossible
		state.InstructorTimeBadness[InstructorTime{section.Instructor, rtb.Time.Next}] = impossible
	}
	for other, badness := range section.Course.Conflicts {
		old := state.CourseTimeBadness[CourseTime{other, rtb.Time}]
		state.CourseTimeBadness[CourseTime{other, rtb.Time}] = worst(old, badness)
		if section.Course.TwoSlots {
			old := state.CourseTimeBadness[CourseTime{other, rtb.Time.Next}]
			state.CourseTimeBadness[CourseTime{other, rtb.Time.Next}] = worst(old, badness)
		}
	}

	// report the pick
	assignment := &CoursePlacement{
		Course:  section.Course,
		Room:    rtb.Room,
		Time:    rtb.Time,
		Badness: rtb.Badness,
	}
	result.Badness += rtb.Badness.N
	result.Schedule = append(result.Schedule, assignment)
	state.Solve(head+1, result)
}

func round(d time.Duration, nearest time.Duration) time.Duration {
	if nearest <= 1 {
		return d
	}
	r := d % nearest
	if r+r >= nearest {
		return d - r + nearest
	}
	return d - r
}

func Complain(data *DataSet, result *SearchResult) {
	if result.Badness < 0 {
		return
	}

	// penalize instructors with spread out schedules on a given day
	instructorToPlacements := make(map[*Instructor][]*CoursePlacement)
	for _, elt := range result.Schedule {
		lst := instructorToPlacements[elt.Course.Instructor]
		instructorToPlacements[elt.Course.Instructor] = append(lst, elt)
	}

	for _, placements := range instructorToPlacements {
		sort.Slice(placements, func(a, b int) bool {
			return placements[a].Time.Position < placements[b].Time.Position
		})

		for i, a := range placements[:len(placements)-1] {
			b := placements[i+1]
			aBreak := strings.IndexAny(a.Time.Name, "0123456789")
			bBreak := strings.IndexAny(b.Time.Name, "0123456789")
			if aBreak < 1 || bBreak < 1 || a.Time.Name[:aBreak] != b.Time.Name[:bBreak] {
				continue
			}

			gap := b.Time.Position - a.Time.Position
			if gap < 2 {
				continue
			}
			result.Badness += gap * gap
		}
	}
}
