package postgresql

import (
	"bytes"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os/exec"
	"sync"
	"time"
)

const (
	maximumPort = 60000
	minimumPort = 10000
)

var (
	randomPortCache = make(map[string]int)
	randomPortLock  = sync.Mutex{}

	runningCommandsLock = &sync.Mutex{}
	runningCommands     = make(map[int]chan error)
)

func connectToJumpHost(config *Config) error {
	runningCommandsLock.Lock()
	defer runningCommandsLock.Unlock()
	if _, found := runningCommands[config.TunneledPort]; found {
		log.Println("[DEBUG] Reusing jumphost process")
		return nil
	}
	log.Println("[DEBUG] Connecting to jumphost")
	args := []string{
		config.JumpHost,
		"-o", "UserKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=no",
		"-L", fmt.Sprintf("127.0.0.1:%d:%s:%d", config.TunneledPort, config.Host, config.Port),
		"-N",
	}
	var combinedOutput bytes.Buffer

	log.Printf("[DEBUG] Calling ssh with %v\n", args)
	cmd := exec.CommandContext(config.ctx, "ssh")
	cmd.Args = append(cmd.Args, args...)
	cmd.Stderr = &combinedOutput
	cmd.Stdout = &combinedOutput
	err := cmd.Start()
	if err != nil {
		return fmt.Errorf("failed to start ssh tunnel: %w", err)
	}
	errorChannel := make(chan error)
	go func() {
		errorChannel <- cmd.Wait()
	}()
	for try := 0; try < 10; try++ {
		select {
		case err := <-errorChannel:
			return fmt.Errorf("ssh exited: %w %s", err, combinedOutput.String())
		case <-time.After(time.Millisecond * 100):
			_, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", config.TunneledPort))
			if err == nil {
				log.Printf("[DEBUG] Sucessfuly connected on tunnel port %d\n", config.TunneledPort)
				runningCommands[config.TunneledPort] = errorChannel
				return nil
			}
			log.Printf("[DEBUG] Failed to connect on tunnel port %d\n", config.TunneledPort)
			time.Sleep(1 * time.Second)
		}
	}

	return fmt.Errorf("ssh failed to connect: %s", combinedOutput.String())
}

func getRandomPort(cacheKey string) int {
	if port, ok := randomPortCache[cacheKey]; ok {
		return port
	}
	randomPortLock.Lock()
	defer randomPortLock.Unlock()
	result := rand.Intn(maximumPort-minimumPort) + minimumPort
	randomPortCache[cacheKey] = result
	return result
}

func WaitForRunningCommands() {
	for _, errorChannel := range runningCommands {
		<-errorChannel
	}
}
