package gorpc

import (
	"fmt"
	"reflect"
	"testing"
)

type Foo int

type Args struct{ Num1, Num2 int }

func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

// it's not a exported Method
func (f Foo) sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

func _assert(condition bool, msg string, v ...interface{}) {
	if !condition {
		panic(fmt.Sprintf("assertion failed: "+msg, v...))
	}
}

// test service is exported and return type correct
func TestNewService(t *testing.T) {
	var foo Foo
	s := newService(&foo)
	_assert(len(s.methodMap) == 1, "wrong service Method, expect 1, but got %d", len(s.methodMap))
	mType := s.methodMap["Sum"]
	_assert(mType != nil, "wrong Method, Sum shouldn't nil")
}

// test dynamic call works
func TestMethodType_Call(t *testing.T) {
	var foo Foo
	s := newService(&foo)
	mType := s.methodMap["Sum"]

	argv := mType.newArgv()
	replyv := mType.newReplyv()
	argv.Set(reflect.ValueOf(Args{Num1: 1, Num2: 3}))
	err := s.call(mType, argv, replyv)
	// This part of the expression is typically used in the context of reflection in Go. Assuming replyv is a reflect.Value object, the method Interface() returns the underlying value as an interface{}. This allows you to work with the concrete value stored in a reflect.Value.
	// (*int):
	// This part is a type assertion. It asserts that the interface value returned by replyv.Interface() is a pointer to an int. If the assertion is successful, it converts the interface to a *int. If it fails (meaning the interface does not actually hold a *int), it will cause a runtime panic.
	// * (dereference operator):
	// This operator is used to dereference the pointer obtained from the type assertion. Dereferencing a pointer gives you access to the value that the pointer points to. In this case, * is used to access the int value that the pointer holds
	_assert(err == nil && *replyv.Interface().(*int) == 4 && mType.NumCalls() == 1, "failed to call Foo.Sum")
}
