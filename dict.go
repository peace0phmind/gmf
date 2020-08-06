package gmf

/*

#cgo pkg-config: libavutil

#include "stdlib.h"
#include "libavutil/dict.h"

*/
import "C"

import (
	"log"
	"unsafe"
)

type Pair struct {
	Key string
	Val string
}

const (
	AV_DICT_MATCH_CASE      = C.AV_DICT_MATCH_CASE
	AV_DICT_IGNORE_SUFFIX   = C.AV_DICT_IGNORE_SUFFIX
	AV_DICT_DONT_STRDUP_KEY = C.AV_DICT_DONT_STRDUP_KEY
	AV_DICT_DONT_STRDUP_VAL = C.AV_DICT_DONT_STRDUP_VAL
	AV_DICT_DONT_OVERWRITE  = C.AV_DICT_DONT_OVERWRITE
	AV_DICT_APPEND          = C.AV_DICT_APPEND
	AV_DICT_MULTIKEY        = C.AV_DICT_MULTIKEY
)

type Dict struct {
	avDict *C.struct_AVDictionary
}

func NewDict(pairs []Pair) *Dict {
	this := &Dict{avDict: nil}

	for _, pair := range pairs {
		if err := this.Set(pair.Key, pair.Val, 0); err != nil {
			log.Fatalf("Set dict error: %s", err)
			return nil
		}
	}

	return this
}

func (d *Dict) Count() int {
	if d.avDict == nil {
		return 0
	}

	return int(C.av_dict_count(d.avDict))
}

func (d *Dict) Set(key, value string, flags int) error {
	ckey := C.CString(key)
	defer C.free(unsafe.Pointer(ckey))
	cval := C.CString(value)
	defer C.free(unsafe.Pointer(cval))

	if ret := C.av_dict_set(&d.avDict, ckey, cval, C.int(flags)); int(ret) < 0 {
		log.Printf("unable to set: key '%v' value '%v', error: %s\n", key, value, AvError(int(ret)))
		return AvError(int(ret))
	}

	return nil
}

func (d *Dict) SetInt(key string, value int, flags int) error {
	ckey := C.CString(key)
	defer C.free(unsafe.Pointer(ckey))

	if ret := C.av_dict_set_int(&d.avDict, ckey, C.int64_t(value), C.int(flags)); int(ret) < 0 {
		log.Printf("unable to set int: key '%v' value '%d', error: %s\n", key, value, AvError(int(ret)))
		return AvError(int(ret))
	}

	return nil
}

func (d *Dict) Free() {
	if d.avDict != nil {
		//C.av_dict_free(&d.avDict)
	}
}
