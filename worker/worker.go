package main

import (
	"fmt"
	"net"
	"net/rpc"
	"os"

	"uk.ac.bris.cs/gameoflife/gol"
)

type GOLWorker struct {
}

// structs for our channels used to communicate with the worker goroutine
type threadJob struct {
	startY int
	endY   int
	world  [][]byte
}

type threadResult struct {
	ID           int
	startY       int
	worldSection [][]byte
}

type section struct {
	start int
	end   int
}

// process the section the broker gives
func (e *GOLWorker) ProcessSection(req gol.SectionRequest, res *gol.SectionResponse) error {
	p := req.Params
	world := req.World

	startY := req.StartY
	endY := req.EndY

	rows := endY - startY

	// channels to send work and recieve work in parallel
	jobChan := make(chan threadJob)
	resultChan := make(chan threadResult, p.Threads)

	// split the rows assigned to a worker between the threads
	threadSections := assignRows(rows, p.Threads)

	// call goroutine to work on each row
	for i := 0; i < p.Threads; i++ {
		go processThread(i, p, jobChan, resultChan)
	}

	// build correct section of world that the worker was supposed to process
	// return said section
	for _, job := range threadSections {
		jobChan <- threadJob{
			// relative to node's section of rows
			startY: job.start + startY,
			endY:   job.end + startY,
			world:  world,
		}
	}
	close(jobChan)

	// collect all the resuts and put them into the new state of world
	results := make([]threadResult, 0, len(threadSections))
	for i := 0; i < len(threadSections); i++ {
		results = append(results, <-resultChan)
	}
	close(resultChan)

	// build correct new section of world
	updatedSection := make([][]byte, rows)
	for _, result := range results {
		offset := result.startY - startY
		for i, row := range result.worldSection {
			updatedSection[offset+i] = row
		}
	}

	// update response to give back to broker
	res.StartY = startY
	res.Section = updatedSection
	return nil
}

// helper func to make worker shut down on keypress
func (e *GOLWorker) Shutdown(_ struct{}, _ *struct{}) error {
	fmt.Println("shutdown signal recieved, stopping worker.")

	go func() {
		os.Exit(0)
	}()
	return nil
}

func calculateNextStates(p gol.Params, world [][]byte, startY, endY int) [][]byte {
	h := p.ImageHeight //h rows
	w := p.ImageWidth  //w columns

	rows := endY - startY

	//make new grid section
	newRows := make([][]byte, rows)
	for i := 0; i < rows; i++ {
		newRows[i] = make([]byte, w)
	}

	for i := startY; i < endY; i++ {
		for j := 0; j < w; j++ { //accessing each individual cell
			count := 0
			up := (i - 1 + h) % h
			down := (i + 1) % h
			left := (j - 1 + w) % w
			right := (j + 1) % w

			//need to check all it's neighbors and state of it's cell
			leftCell := world[i][left]
			if leftCell == 255 {
				count += 1
			}
			rightCell := world[i][right]
			if rightCell == 255 {
				count += 1
			}
			upCell := world[up][j]
			if upCell == 255 {
				count += 1
			}
			downCell := world[down][j]
			if downCell == 255 {
				count += 1
			}
			upRightCell := world[up][right]
			if upRightCell == 255 {
				count += 1
			}
			upLeftCell := world[up][left]
			if upLeftCell == 255 {
				count += 1
			}

			downRightCell := world[down][right]
			if downRightCell == 255 {
				count += 1
			}

			downLeftCell := world[down][left]
			if downLeftCell == 255 {
				count += 1
			}

			//update the cells
			if world[i][j] == 255 {
				if count == 2 || count == 3 {
					newRows[i-startY][j] = 255
				} else {
					newRows[i-startY][j] = 0
				}
			}

			if world[i][j] == 0 {
				if count == 3 {
					newRows[i-startY][j] = 255
				} else {
					newRows[i-startY][j] = 0
				}
			}

		}
	}
	return newRows
}

func main() {

	err := rpc.RegisterName("GOLWorker", new(GOLWorker))

	if err != nil {
		fmt.Println("Error registering RPC:", err)
		return
	}

	listener, err := net.Listen("tcp4", "0.0.0.0:8030")
	if err != nil {
		fmt.Println("Error starting listener:", err)
		os.Exit(1)
		return
	}
	fmt.Println("Worker listening on port 8030 (IPv4)...")

	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("Connection error:", err)
			continue
		}
		go rpc.ServeConn(conn)
	}
}

// helper function, calls calculateNextState on each section the thread is working on
func processThread(id int, p gol.Params, jobs <-chan threadJob, results chan<- threadResult) {
	for job := range jobs {
		outputSection := calculateNextStates(p, job.world, job.startY, job.endY)
		results <- threadResult{
			ID:           id,
			startY:       job.startY,
			worldSection: outputSection,
		}
	}
}

// helper function to split rows between threads for one worker
func assignRows(height, threads int) []section {
	// say if we had 16 rows and 4 threads
	// we want to be able to allocate say 4 rows to 1 thread, 4 to the other thread etc.

	// we need to calculate the minimum number of rows for each worker
	minRows := height / threads
	// then say if we have extra rows left over then we need to assign those evenly to each worker
	extraRows := height % threads

	// make a slice, the size of the number of threads
	sections := make([]section, threads)
	start := 0

	for i := 0; i < threads; i++ {
		// assigns the base amount of rows to the thread
		rows := minRows
		// if say we're on worker 2 and there are 3 extra rows left,
		// then we can add 1 more job to the thread
		if i < extraRows {
			rows++
		}

		// marks where the end of the section ends
		end := start + rows
		// assigns these rows to the section
		sections[i] = section{start: start, end: end}
		// start is updated for the next worker
		start = end
	}
	return sections
}
