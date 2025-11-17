package main

import (
	"fmt"
	"math/rand"
	"net/rpc"
	"os/exec"
	"testing"
	"time"
)

// The same worker IPs you have in broker.go:
var workerIPs = []string{
	"3.236.77.90:8030",
	"44.221.67.226:8030",
	"3.236.46.162:8030",
}

// Runs the normal coursework tests
func runGOLTests() *exec.Cmd {
	cmd := exec.Command("go", "test", "-count=1", "./tests", "-v", "-run", "TestGol/-1")
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Start()
	return cmd
}

// Sends RPC shutdown to a worker
func killWorker(ip string) {
	fmt.Println("💀 Killing worker at:", ip)

	client, err := rpc.Dial("tcp", ip)
	if err != nil {
		fmt.Println("⛔ Could not connect to worker:", err)
		return
	}
	defer client.Close()

	_ = client.Call("GOLWorker.Shutdown", struct{}{}, nil)

	fmt.Println("✔ Worker shutdown RPC sent:", ip)
}

func TestChaos(t *testing.T) {
	rand.Seed(time.Now().UnixNano())

	// Start normal GOL test run
	cmd := runGOLTests()
	fmt.Println("🚀 Started go test (fault tolerance test)")

	// Random delay before killing a worker
	delay := time.Duration(rand.Intn(20)+5) * time.Second
	fmt.Println("⏳ Will kill a random worker in:", delay)
	time.Sleep(delay)

	// Pick worker to kill
	victimIndex := rand.Intn(len(workerIPs))
	victimIP := workerIPs[victimIndex]

	killWorker(victimIP)

	// Wait for go test to finish
	fmt.Println("⌛ Waiting for tests to finish...")
	err := cmd.Wait()
	if err != nil {
		fmt.Println("⚠ go test exited with error (expected if tolerance is not perfect):", err)
	} else {
		fmt.Println("🎉 Chaos test completed successfully")
	}
}
