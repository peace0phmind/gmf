package gmf

import (
	"testing"
)

func TestDict(t *testing.T) {
	d := NewDict([]Pair{})

	d.Set("aaa", "bbb", AV_DICT_DONT_OVERWRITE)
	d.Set("bbb", "ccc", AV_DICT_DONT_OVERWRITE)
	d.Set("ddd", "eee", AV_DICT_DONT_OVERWRITE)
	d.Set("aaa", "bbb", AV_DICT_DONT_OVERWRITE)

	d.Dump()
}
