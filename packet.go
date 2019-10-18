package rtmpgo

import (
	"encoding/binary"
	"io"
)

var (
	AmfNumber     = 0x00
	AmfBoolean    = 0x01
	AmfString     = 0x02
	AmfObject     = 0x03
	AmfNull       = 0x05
	AmfArrayNull  = 0x06
	AmfMixedArray = 0x08
	AmfEnd        = 0x09
	AmfArray      = 0x0a
	AmfInt8       = 0x0100
	AmfInt16      = 0x0101
	AmfInt32      = 0x0102
	AmfVariant    = 0x0103
)

type AMFObj struct {
	aType int
	str   string
	i     int
	buf   []byte
	obj   map[string]AMFObj
	f64   float64
}

func ReadAMF(r io.Reader) (a AMFObj) {
	a.aType = ReadInt(r, 1)
	switch a.aType {
	case AmfString:
		n := ReadInt(r, 2)
		b := ReadBuf(r, n)
		a.str = string(b)
	case AmfNumber:
		_ = binary.Read(r, binary.BigEndian, &a.f64)
	case AmfBoolean:
		a.i = ReadInt(r, 1)
	case AmfMixedArray:
		ReadInt(r, 4)
		fallthrough
	case AmfObject:
		a.obj = map[string]AMFObj{}
		for {
			n := ReadInt(r, 2)
			if n == 0 {
				break
			}
			name := string(ReadBuf(r, n))
			a.obj[name] = ReadAMF(r)
		}
	case AmfArray, AmfVariant:
		panic("amf: read: unsupported array or variant")
	case AmfInt8:
		a.i = ReadInt(r, 1)
	case AmfInt16:
		a.i = ReadInt(r, 2)
	case AmfInt32:
		a.i = ReadInt(r, 4)
	}
	return
}

func WriteAMF(r io.Writer, a AMFObj) {
	WriteInt(r, a.aType, 1)
	switch a.aType {
	case AmfString:
		WriteInt(r, len(a.str), 2)
		_, _ = r.Write([]byte(a.str))
	case AmfNumber:
		_ = binary.Write(r, binary.BigEndian, a.f64)
	case AmfBoolean:
		WriteInt(r, a.i, 1)
	case AmfMixedArray:
		_, _ = r.Write(a.buf[:4])
	case AmfObject:
		for name, val := range a.obj {
			WriteInt(r, len(name), 2)
			_, _ = r.Write([]byte(name))
			WriteAMF(r, val)
		}
		WriteInt(r, 9, 3)
	case AmfArray, AmfVariant:
		panic("amf: write unsupported array, var")
	case AmfInt8:
		WriteInt(r, a.i, 1)
	case AmfInt16:
		WriteInt(r, a.i, 2)
	case AmfInt32:
		WriteInt(r, a.i, 4)
	}
}
