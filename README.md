## GoRetrier - A non sophisticated retry module in Golang.

### Installation
```bash
go get github.com/silverspell/goretrier
```

### How-to

Create a struct that implements a Retrieable interface (has an Exec () error function). 
Then simply create a pointer of the Retrier struct using New() function and call the Start() method.

```go
package main

import (
	"errors"
	"fmt"
	"time"

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

	r.Start()
	r2.Start()
	time.Sleep(10 * time.Second)
}
```
