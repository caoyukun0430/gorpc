package gorpc

import (
	"go/ast"
	"log"
	"reflect"
	"sync/atomic"
)

// rely on reflect.Value to invoke functions or methods dynamically, which is useful in scenarios like RPC,
// where you might not know the exact function signatures at compile time.

// standard func for RPC call: func (t *T) MethodName(argType T1, replyType *T2) error

// each methodType contains full info of a RPC method

type methodType struct {
	method    reflect.Method // reflect.Method is the method itself, has fields like Name, Type, and Func that provide information about a method.
	ArgType   reflect.Type
	ReplyType reflect.Type
	numCalls  uint64
}

// method newArgv and newReplyv to create argv and replyv instance to be used for rpc call

func (m *methodType) NumCalls() uint64 {
	return atomic.LoadUint64(&m.numCalls)
}

func (m *methodType) newArgv() reflect.Value {
	var argv reflect.Value
	// arg may be a pointer type, or a value type, differs when dereference with .Elem()
	// ArgType is *int, m.ArgType.Elem() is int, and reflect.New(int) creates a *int.
	if m.ArgType.Kind() == reflect.Ptr {
		argv = reflect.New(m.ArgType.Elem())
	} else {
		// ArgType is int,reflect.New() is *int, and .Elem() creates a int.
		argv = reflect.New(m.ArgType).Elem()
	}
	return argv
}

func (m *methodType) newReplyv() reflect.Value {
	// reply MUST be a pointer type so that direct modification and avoid copies
	replyv := reflect.New(m.ReplyType.Elem())
	switch m.ReplyType.Elem().Kind() {
	case reflect.Map:
		replyv.Elem().Set(reflect.MakeMap(m.ReplyType.Elem()))
	case reflect.Slice:
		replyv.Elem().Set(reflect.MakeSlice(m.ReplyType.Elem(), 0, 0))
	}
	return replyv
}

//	define rpc service e.g. service T {
//	    "ServiceMethod"： "T.MethodName"
//	    "Argv"："0101110101..."
//	}
type service struct {
	name      string
	typ       reflect.Type
	rcvr      reflect.Value // instance itself, preserve receiver itself because call need to use it as 0th argv
	methodMap map[string]*methodType
}

func newService(rcvr interface{}) *service {
	s := new(service)
	s.rcvr = reflect.ValueOf(rcvr)
	// reflect.Indirect(s.rcvr) dereferences s.rcvr if it is a pointer.
	// .Type() gets the reflection type of the dereferenced value.
	// .Name() gets the name of the type.
	// If s.rcvr is a pointer not a struct, s.typ.Name() is empty string. so we can't simply do this
	s.name = reflect.Indirect(s.rcvr).Type().Name()
	s.typ = reflect.TypeOf(rcvr)
	if !ast.IsExported(s.name) {
		log.Fatalf("rpc server: %s is not a valid service name", s.name)
	}
	s.registerMethods()
	return s
}

// filter methods that meets the requirement:
// two exported/built-in type argv, the 0th is instance itself, like py self
// one error type replyv
func (s *service) registerMethods() {
	s.methodMap = make(map[string]*methodType)
	for i := 0; i < s.typ.NumMethod(); i++ {
		method := s.typ.Method(i)
		mType := method.Type
		if mType.NumIn() != 3 || mType.NumOut() != 1 {
			continue
		}
		if mType.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			continue
		}
		argType, replyType := mType.In(1), mType.In(2)
		if !isExportedOrBuiltinType(argType) || !isExportedOrBuiltinType(replyType) {
			continue
		}
		s.methodMap[method.Name] = &methodType{
			method:    method,
			ArgType:   argType,
			ReplyType: replyType,
		}
		log.Printf("rpc server: register %s.%s\n", s.name, method.Name)
	}

}

// Builtin Types: For built-in types (like int, string, etc.), PkgPath returns an empty string "". This is because built-in types are not defined in any user-defined package.
func isExportedOrBuiltinType(t reflect.Type) bool {
	return ast.IsExported(t.Name()) || t.PkgPath() == ""
}

// Call allows you to invoke functions or methods dynamically, which is useful in scenarios like RPC, where you might not know the exact function signatures at compile time.
func (s *service) call(m *methodType, argv, replyv reflect.Value) error {
	atomic.AddUint64(&m.numCalls, 1)
	f := m.method.Func
	// Call takes a slice of reflect.Value as its argument. Each element in this slice represents an argument to be passed to the function or method.
	returnValues := f.Call([]reflect.Value{s.rcvr, argv, replyv})
	if errInter := returnValues[0].Interface(); errInter != nil {
		return errInter.(error)
	}
	return nil
}
