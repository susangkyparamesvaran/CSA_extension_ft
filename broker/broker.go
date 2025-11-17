package main

import (
	"fmt"
	"net"
	"net/rpc"
	"os"
	"sync"

	"uk.ac.bris.cs/gameoflife/gol"
)

// the broker will keep track of the multiple GOLWorkers
// can use to tell us how many workers we have and then split up the image based on that
type Broker struct {
	workerAddresses []string
	alive           int

	// this is to protect workerAddresses and alive count
	mu sync.RWMutex
}

type section struct {
	start int
	end   int
}

// assign section helper function from before
// helper func to assign sections of image to workers based on no. of threads
func assignSections(height, workers int) []section {

	// we need to calculate the minimum number of rows for each worker
	minRows := height / workers
	// then say if we have extra rows left over then we need to assign those evenly to each worker
	extraRows := height % workers

	// make a slice, the size of the number of threads
	sections := make([]section, workers)
	start := 0

	for i := 0; i < workers; i++ {
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

// one iteration of the game using all workers
func (broker *Broker) ProcessSection(req gol.BrokerRequest, res *gol.BrokerResponse) error {
	p := req.Params
	world := req.World

	// make a copy of the current worker addresses safely using mutex
	broker.mu.RLock()
	workerAddresses := append([]string(nil), broker.workerAddresses...)
	broker.mu.RUnlock()

	numWorkers := len(workerAddresses)

	// throw an error in teh case of there not being any workers dialled
	if numWorkers == 0 {
		return fmt.Errorf("no workers registered")
	}

	// assign different sections of the image to each worker (aws node)
	sections := assignSections(p.ImageHeight, numWorkers)

	type sectionResult struct {
		start       int
		rows        [][]byte
		workerIndex int
		err         error
	}

	resultsChan := make(chan sectionResult, numWorkers)

	// for each worker, assign the sections
	for i, address := range workerAddresses {
		section := sections[i]
		index := i
		address := address

		// process for each worker
		go func() {

			client, err := rpc.Dial("tcp", address)
			if err != nil {
				resultsChan <- sectionResult{workerIndex: index,
					err: fmt.Errorf("dial %s: %w", address, err),
				}
				return
			}

			defer client.Close()

			// section request
			sectionReq := gol.SectionRequest{
				Params: p,
				World:  world,
				StartY: section.start,
				EndY:   section.end,
			}

			var sectionRes gol.SectionResponse

			if err := client.Call("GOLWorker.ProcessSection", sectionReq, &sectionRes); err != nil {
				resultsChan <- sectionResult{
					workerIndex: index,
					err:         fmt.Errorf("dial %s: %w", address, err),
				}
				return
			}

			resultsChan <- sectionResult{
				start:       sectionRes.StartY,
				rows:        sectionRes.Section,
				workerIndex: index,
				err:         nil,
			}

		}()
	}

	results := make([]sectionResult, numWorkers)

	// make a map - has which worker indices from the workerAddresses have failed
	failedWorkers := make(map[int]error)

	for i := 0; i < numWorkers; i++ {
		results[i] = <-resultsChan
		// this meams rpc dial/call failed which means that the worker is dead
		if results[i].err != nil {
			failedWorkers[results[i].workerIndex] = results[i].err
		}
	}

	close(resultsChan)

	// check if any workers have failed, then we need to finish the turn locally
	if len(failedWorkers) > 0 {
		// finish turn locally
		newWorld := nextWorldBackup(p, world)
		res.World = newWorld

		// remove failed workers from workerAddresses
		broker.mu.Lock()
		workingNodes := make([]string, 0, len(broker.workerAddresses))

		for i, address := range broker.workerAddresses {
			// check if the worker index exists in failedWorkers map
			_, isFailed := failedWorkers[i]

			// if the node still works, then append to workingNodes
			if !(isFailed) {
				workingNodes = append(workingNodes, address)
			}
		}

		broker.workerAddresses = workingNodes
		broker.mu.Unlock()

		return nil
	}

	// build new world from the individual sections
	newWorld := make([][]byte, p.ImageHeight)
	for _, result := range results {
		for i, row := range result.rows {
			newWorld[result.start+i] = row
		}
	}

	res.World = newWorld
	return nil
}

func (broker *Broker) GetAliveCount(_ struct{}, out *int) error {
	*out = broker.alive
	return nil
}

// We need a function that when q (quit) is pressed then the controller
// exit without killing the simulation
// when q is pressed we need to save the current board (pgm), then call a function that
// doesnt persist the world -> basically do nothing
func (broker *Broker) ControllerExit(_ gol.Empty, _ *gol.Empty) error {
	return nil
}

// when k is pressed, we need to call a function that would send GOL.Shutdown
// to each worker and then kill itself
// then the controller saves the final image and exits
func (broker *Broker) KillWorkers(_ gol.Empty, _ *gol.Empty) error {
	for _, address := range broker.workerAddresses {
		if c, err := rpc.Dial("tcp", address); err == nil {
			_ = c.Call("GOLWorker.Shutdown", struct{}{}, nil)
			_ = c.Close()
		}
	}

	go os.Exit(0)
	return nil
}

// have a helper function that will be the backup if a worker fails during a turn
// this is basically just the implementation of calculateNextStates but for the whole board
func nextWorldBackup(p gol.Params, world [][]byte) [][]byte {
	h := p.ImageHeight //h rows
	w := p.ImageWidth  //w columns

	//make new world
	newWorld := make([][]byte, h)
	for i := 0; i < h; i++ {
		newWorld[i] = make([]byte, w)
	}

	for i := 0; i < h; i++ {
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
					newWorld[i][j] = 255
				} else {
					newWorld[i][j] = 0
				}
			}

			if world[i][j] == 0 {
				if count == 3 {
					newWorld[i][j] = 255
				} else {
					newWorld[i][j] = 0
				}
			}

		}
	}
	return newWorld
}

func main() {

	broker := &Broker{
		workerAddresses: []string{
			"3.236.77.90:8030",
			"44.221.67.226:8030",
			"3.236.46.162:8030",
		},
	}

	err := rpc.RegisterName("Broker", broker)

	if err != nil {
		fmt.Println("Error registering RPC:", err)
		os.Exit(1)
		return
	}

	listener, err := net.Listen("tcp4", "0.0.0.0:8040")
	if err != nil {
		fmt.Println("Error starting listener:", err)
		os.Exit(1)
		return
	}
	fmt.Println("Broker listening on port 8040 (IPv4)...")

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
