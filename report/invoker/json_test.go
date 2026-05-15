package invoker

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

type TestStruct struct {
	Name   string
	Age    int
	Addr   string
	Values []TestSubStruct
}

type TestSubStruct struct {
	A1 string
	A2 string
	A3 int
}

func TestValid(t *testing.T) {

	t1 := &TestStruct{
		Name:   "TTT",
		Age:    0,
		Addr:   "TTT",
		Values: nil,
	}

	t2 := &TestStruct{
		Name: "TTT",
		Age:  0,
		Addr: "TTT",
		Values: []TestSubStruct{{
			A1: "TTT",
			A2: "TT",
			A3: 0,
		}},
	}

	t3 := &TestStruct{
		Name:   "TTT",
		Age:    0,
		Addr:   "TTT",
		Values: []TestSubStruct{},
	}

	assert.Error(t, valid(t1))

	assert.NoError(t, valid(t2))
	assert.Error(t, valid(t3))

}
