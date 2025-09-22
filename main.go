package main

import "fmt"

func main() {
	for i := 0; i < 50; i++ {
		if i == 20 {
			continue
		}
		fmt.Println(i)
	}

	for i := 20; i < 10; i++ {
		fmt.Println()
	}
}
