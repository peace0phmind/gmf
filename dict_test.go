package gmf

import (
	"testing"
)

func TestDict(t *testing.T) {
	d := NewDict([]Pair{{"a1","b1"}, {"a1", "b2"}})

	d.Set("aaa", "bbb", AV_DICT_DONT_OVERWRITE)
	d.Set("bbb", "ccc", AV_DICT_DONT_OVERWRITE)
	d.Set("ddd", "eee", AV_DICT_DONT_OVERWRITE)
	d.Set("aaa", "bbb", AV_DICT_DONT_OVERWRITE)

	d.Dump()
	d.Free()
}
