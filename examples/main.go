package main

import (
	"errors"
	"fmt"
	"sync"

	retrier "github.com/silverspell/goretrier"
)

type MyTask struct {
	myTitle string
}

func (m *MyTask) Exec() error {
	if m.myTitle == "AAA" {
		fmt.Println("Error. myTitle should not be AAA")
		return errors.New("Unknown error")
	}
	fmt.Printf("My Title:%s\n", m.myTitle)
	return nil
}

func Callback(r *retrier.Retrier) {
	if r.Err() != nil {
		fmt.Println("Callback: ", r.Err())
		return
	}
	fmt.Println("Callback: ", "No error")
}

func main() {
	t := &MyTask{
		myTitle: "BBB",
	}

	t2 := &MyTask{
		myTitle: "AAA",
	}

	// t is the Retriable interface, 3 is the maximum attempts, 1000 is the milliseconds delay between attempts.
	r, err := retrier.New(t, 3, 1000)
	if err != nil {
		panic(err)
	}
	// t2 is the Retriable interface, 5 is the maximum attempts, 500 is the milliseconds delay between attempts.
	r2, err := retrier.New(t2, 5, 500)
	if err != nil {
		panic(err)
	}

	wg := &sync.WaitGroup{}

	r.Start(wg, Callback)
	r2.Start(wg, func(r *retrier.Retrier) {
		if r.Err() != nil {
			fmt.Printf("r2 task is failed after %d retries. error: %s\n", r.Attempts(), r.Err())
			return
		}
		fmt.Println("r2 task is done sucessfully")
	})
	wg.Wait()
}
