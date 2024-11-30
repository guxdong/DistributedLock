package distributedlock

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestNewRedis(t *testing.T) {
	ip := ""
	passowrd := ""
	port := 6379
	client := NewRedis(ip, passowrd, port)
	lock := NewRedisLock(client, 10, true, true)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer func() {
			wg.Done()
		}()
		err := lock.Lock("distributed_lock")
		if err != nil {
			fmt.Println("goroutine 1 lock failed, err:", err)
			return
		}
		fmt.Println("goroutine 1 lock success")
		time.Sleep(5 * time.Second)
		lock.UnLock("distributed_lock")
	}()

	go func() {
		defer func() {
			wg.Done()
		}()
		err := lock.Lock("distributed_lock")
		if err != nil {
			fmt.Println("goroutine 2 lock failed, err:", err)
			return
		}
		fmt.Println("goroutine 2 lock success")
		time.Sleep(5 * time.Second)
		lock.UnLock("distributed_lock")
	}()

	wg.Wait()
}
