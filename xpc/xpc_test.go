package xpc

import (
	"fmt"
	"testing"
)

func checkUUID(t *testing.T, v interface{}) UUID {
	if uuid, ok := v.(UUID); ok {
		return uuid
	}
	t.Errorf("not a UUID: %#v", v)
	return UUID{}
}

func TestConvertUUID(t *testing.T) {
	uuid := MakeUUID("00112233445566778899aabbccddeeff")

	xv := goToXpc(uuid)
	v := xpcToGo(xv)

	xpc_release(xv)

	uuid2 := checkUUID(t, v)

	if uuid != uuid2 {
		t.Errorf("want %#v, got %#v", uuid, uuid2)
	}
}

func TestConvertSlice(t *testing.T) {
	arr := []string{"one", "two", "three"}

	xv := goToXpc(arr)
	v := xpcToGo(xv)

	xpc_release(xv)

	arr2, ok := v.(Array)
	if !ok {
		t.Fatalf("not an array: %#v", v)
	}
	if len(arr) != len(arr2) {
		t.Fatalf("want %#v, got %#v", arr, arr2)
	}
	for i := range arr {
		t.Run(fmt.Sprintf("array[%d]", i), func(t *testing.T) {
			if arr[i] != arr2[i] {
				t.Errorf("want %#v, got %#v", arr[i], arr2[i])
			}
		})
	}
}

func TestConvertSliceUUID(t *testing.T) {
	arr := []UUID{
		MakeUUID("0000000000000000"),
		MakeUUID("1111111111111111"),
		MakeUUID("2222222222222222"),
	}

	xv := goToXpc(arr)
	v := xpcToGo(xv)

	xpc_release(xv)

	arr2, ok := v.(Array)
	if !ok {
		t.Fatalf("not an array: %#v", v)
	}
	if len(arr) != len(arr2) {
		t.Fatalf("want %#v, got %#v", arr, arr2)
	}
	for i := range arr {
		t.Run(fmt.Sprintf("array[%d]", i), func(t *testing.T) {
			uuid1 := checkUUID(t, arr[i])
			uuid2 := checkUUID(t, arr2[i])
			if uuid1 != uuid2 {
				t.Errorf("want %#v, got %#v", arr[i], arr2[i])
			}
		})
	}
}

func TestConvertMap(t *testing.T) {
	d := Dict{
		"number": int64(42),
		"text":   "hello gopher",
		"uuid":   MakeUUID("aabbccddeeff00112233445566778899"),
	}

	xv := goToXpc(d)
	v := xpcToGo(xv)

	xpc_release(xv)

	d2, ok := v.(Dict)
	if !ok {
		t.Fatalf("not a map: %#v", v)
	}
	if len(d) != len(d2) {
		t.Fatalf("want %#v, got %#v", d, d2)
	}
	for k, v := range d {
		t.Run(fmt.Sprintf("map[%s]", k), func(t *testing.T) {
			if v != d2[k] {
				t.Errorf("want %#v, got %#v", v, d2[k])
			}
		})
	}
}
