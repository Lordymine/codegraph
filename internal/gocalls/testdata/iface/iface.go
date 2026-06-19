// Package iface is a test fixture for call-graph precision: a caller that uses a
// concrete *Dog through an Animal interface. A sound-but-imprecise graph (CHA)
// over-approximates the dynamic call to BOTH Dog.Speak and Cat.Speak; a precise
// graph (VTA) keeps only Dog.Speak, because Cat is never instantiated here.
package iface

type Animal interface {
	Speak() string
}

type Dog struct{}

func (Dog) Speak() string { return "woof" }

type Cat struct{}

func (Cat) Speak() string { return "meow" }

// caller builds a concrete Dog and dispatches Speak through the interface.
func caller() string {
	var a Animal = Dog{}
	return a.Speak()
}
