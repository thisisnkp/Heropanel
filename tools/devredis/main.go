//go:build ignore

// devredis starts an in-memory Redis (miniredis) on a fixed address for local
// smoke tests when a real redis-server isn't available. Run with:
//
//	go run tools/devredis/main.go
package main

import (
	"fmt"

	"github.com/alicebob/miniredis/v2"
)

func main() {
	mr := miniredis.NewMiniRedis()
	if err := mr.StartAddr("127.0.0.1:16399"); err != nil {
		panic(err)
	}
	fmt.Println("devredis listening on", mr.Addr())
	select {}
}
