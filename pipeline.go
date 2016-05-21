package main

import (
	"fmt"
	"os"
	"sync"
)

// Stage is the interface implemented by an object that can be added to a
// pipeline to process incoming FileRecords.
// Init() and Close() are run once per goroutine.
type Stage interface {
	Init()
	Run(*FileRecord) error
	Close()
}

// The pipeline processes FileRecords through a sequence of stages. A FileRecord
// is forwarded to the 'log' channel when a stage returns an error, or to the
// 'output' channel otherwise.
//
// 'input' never changes, 'output' gets forwarded on every call to 'Add'.
//
// The advantage of a pipeline design:
// - Ensures log messages grouped by FileRecord and does not require manual log flushing.
// - Removes some parallelization boilerplate such as the channel loops.
// - Makes it easy to change the number of goroutines allocated to the various stages.
type Pipeline struct {
	input  chan *FileRecord
	output chan *FileRecord
	log    chan *FileRecord
	logWg  sync.WaitGroup
}

func NewPipeline(inputQueueSize, logQueueSize int) *Pipeline {
	var p Pipeline
	p.input = make(chan *FileRecord, inputQueueSize)
	p.output = p.input
	p.log = make(chan *FileRecord, logQueueSize)

	p.logWg.Add(1)
	go func() {
		for fr := range p.log {
			fmt.Fprintln(os.Stderr, fr)
		}
		p.logWg.Done()
	}()

	// Return a reference so that the WaitGroup gets referenced properly.
	return &p
}

func (p *Pipeline) Add(NewStage func() Stage, routineCount int) {
	var wg sync.WaitGroup

	// The output queue is the size of the number of producing goroutines. It
	// ensures that routines are not blocking each other.
	out := make(chan *FileRecord, routineCount)

	wg.Add(routineCount)
	for i := 0; i < routineCount; i++ {
		go func(input chan *FileRecord) {
			s := NewStage()
			s.Init()
			for fr := range input {
				err := s.Run(fr)
				if err != nil {
					p.log <- fr
					continue
				}
				out <- fr
			}
			s.Close()
			wg.Done()
		}(p.output)
	}

	// Change output channel after all the routines have been set up to read from
	// the former output channel.
	p.output = out

	// Close channel when all routines are done.
	go func() {
		wg.Wait()
		close(out)
	}()
}

// Close the pipeline to log everything.
// Call it once the input has been fully produced and the output fully consumed.
func (p *Pipeline) Close() {
	close(p.log)
	p.logWg.Wait()
}
