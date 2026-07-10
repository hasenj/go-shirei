package sample

import "fmt"

// greet prints a friendly greeting to the given name.
func greet(name string) {
	fmt.Println("hello", name)
}

func main() {
	greet("world")
}
