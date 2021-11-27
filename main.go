package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	addr      = flag.String("addr", "0.0.0.0", "address to scan for ports")
	fdCount   = flag.Uint("fd", 64, "file descriptor count")
	portRange = flag.String("range", "80-65535", "port range")
)

type sem struct {
	cap       uint
	ch        chan struct{}
	blockTime []time.Duration
	mu        sync.Mutex
}

func newSem(cap uint) *sem {
	ch := make(chan struct{}, cap)
	for i := 0; i < int(*fdCount); i++ {
		ch <- struct{}{}
	}
	return &sem{
		cap: cap,
		ch:  ch,
	}
}
func (s *sem) acquire() {
	now := time.Now()
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.blockTime = append(s.blockTime, time.Since(now))
	}()
	<-s.ch
}

func (s *sem) release() {
	s.ch <- struct{}{}
}

func (s *sem) avgBlockTime() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.blockTime) == 0 {
		return 0
	}
	var sum time.Duration
	for _, t := range s.blockTime {
		sum += t
	}
	return sum / time.Duration(len(s.blockTime))
}

func main() {
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	min, max := parsePortRange(*portRange)
	now := time.Now()
	var wg sync.WaitGroup
	sem := newSem(*fdCount)
	wg.Add(1)
	go func() {
		for i := min; i <= max; i++ {
			wg.Add(1)
			sem.acquire()
			go func(port int) {
				defer func() {
					wg.Done()
					sem.release()
				}()
				conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", *addr, port))
				if err != nil {
					return
				}
				defer func(conn net.Conn) {
					err := conn.Close()
					if err != nil {
						fmt.Println("cannot close connection:", err)
					}
				}(conn)
				fmt.Printf("DIAL %d ✅\n", port)
			}(i)
		}
		wg.Done()
	}()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		done <- struct{}{}
	}()
	select {
	case <-ctx.Done():
	case <-done:
	}
	stop()
	fmt.Printf("time elapsed: %dms\n", time.Since(now).Milliseconds())
	fmt.Printf("block time: %dns\n", sem.avgBlockTime().Nanoseconds())
}

func parsePortRange(s string) (int, int) {
	parts := strings.Split(s, "-")
	if len(parts) != 2 {
		panic("invalid port range")
	}
	min, _ := strconv.Atoi(parts[0])
	max, _ := strconv.Atoi(parts[1])
	return min, max
}
