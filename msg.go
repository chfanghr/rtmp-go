package rtmpgo

import (
	"bytes"
	"fmt"
	"io"
	"log"
)

var (
	MsgChunkSize  = 1
	MsgAbort      = 2
	MsgAck        = 3
	MsgUser       = 4
	MsgAckSize    = 5
	MsgBandwidth  = 6
	MsgEdge       = 7
	MsgAudio      = 8
	MsgVideo      = 9
	MsgAmf3Meta   = 15
	MsgAmf3Shared = 16
	MsgAmf3Cmd    = 17
	MsgAmfMeta    = 18
	MsgAmfShared  = 19
	MsgAmfCmd     = 20
	MsgAggregate  = 22
	MsgMax        = 22
)

var (
	MsgTypeStr = []string{
		"?",
		"CHUNK_SIZE", "ABORT", "ACK",
		"USER", "ACK_SIZE", "BANDWIDTH", "EDGE",
		"AUDIO", "VIDEO",
		"AMF3_META", "AMF3_SHARED", "AFM3_CMD",
		"AMF_META", "AMF_SHARED", "AMF_CMD",
		"AGGREGATE",
	}
)

type chunkHeader struct {
	typeId  int
	mLen    int
	csId    int
	cFmt    int
	ts      int
	tsDelta int
	strId   int
}

func readChunkHeader(r io.Reader) (m chunkHeader) {
	i := ReadInt(r, 1)
	m.cFmt = (i >> 6) & 3
	m.csId = i & 0x3f
	if m.csId == 0 {
		j := ReadInt(r, 1)
		m.csId = j + 64
	}

	if m.csId == 0x3f {
		j := ReadInt(r, 2)
		m.csId = j + 64
	}

	if m.cFmt == 0 {
		m.ts = ReadInt(r, 3)
		m.mLen = ReadInt(r, 3)
		m.typeId = ReadInt(r, 1)
		m.strId = ReadIntLE(r, 4)
	}

	if m.cFmt == 1 {
		m.tsDelta = ReadInt(r, 3)
		m.mLen = ReadInt(r, 3)
		m.typeId = ReadInt(r, 1)
	}

	if m.cFmt == 2 {
		m.tsDelta = ReadInt(r, 3)
	}

	if m.ts == 0xffffff {
		m.ts = ReadInt(r, 4)
	}
	if m.tsDelta == 0xffffff {
		m.tsDelta = ReadInt(r, 4)
	}

	//l.Printf("chunk:   %v", m)

	return
}

const (
	UNKNOWN   = 0
	PLAYER    = 1
	PUBLISHER = 2
)

const (
	WaitExtra = 0
	WaitData  = 1
)

type MsgStream struct {
	r        stream
	Msg      map[int]*Msg
	vts, ats int

	meta           AMFObj
	id             string
	role           int
	stat           int
	app            string
	W, H           int
	strId          int
	extraA, extraV []byte
	que            chan *Msg
	l              *log.Logger
}

type Msg struct {
	chunkHeader
	data *bytes.Buffer

	key   bool
	curts int
}

func (m *Msg) String() string {
	var typeStr string
	if m.typeId < len(MsgTypeStr) {
		typeStr = MsgTypeStr[m.typeId]
	} else {
		typeStr = "?"
	}
	return fmt.Sprintf("%s %d %v", typeStr, m.mLen, m.chunkHeader)
}

var (
	mrSeq = 0
)

func NewMsgStream(r io.ReadWriteCloser) *MsgStream {
	mrSeq++
	return &MsgStream{
		r:   stream{r},
		Msg: map[int]*Msg{},
		id:  fmt.Sprintf("#%d", mrSeq),
	}
}

func (mr *MsgStream) String() string {
	return mr.id
}

func (mr *MsgStream) Close() {
	mr.r.Close()
}

func (mr *MsgStream) WriteMsg(cfmt, csid, typeid, strid, ts int, data []byte) {
	var b bytes.Buffer
	start := 0
	for i := 0; start < len(data); i++ {
		if i == 0 {
			if cfmt == 0 {
				WriteInt(&b, csid, 1)      // fmt=0 csId
				WriteInt(&b, ts, 3)        // ts
				WriteInt(&b, len(data), 3) // message length
				WriteInt(&b, typeid, 1)    // message type id
				WriteIntLE(&b, strid, 4)   // message stream id
			} else {
				WriteInt(&b, 0x1<<6+csid, 1) // fmt=1 csId
				WriteInt(&b, ts, 3)          // tsDelta
				WriteInt(&b, len(data), 3)   // message length
				WriteInt(&b, typeid, 1)      // message type id
			}
		} else {
			WriteBuf(&b, []byte{0x3<<6 + byte(csid)}) // fmt=3, csId
		}
		size := 128
		if len(data)-start < size {
			size = len(data) - start
		}
		WriteBuf(&b, data[start:start+size])
		WriteBuf(mr.r, b.Bytes())
		b.Reset()
		start += size
	}
	l.Printf("Msg: csId %d ts %d paylen %d", csid, ts, len(data))
}

func (mr *MsgStream) WriteAudio(strid, ts int, data []byte) {
	d := append([]byte{0xaf, 1}, data...)
	tsdelta := ts - mr.ats
	mr.ats = ts
	mr.WriteMsg(1, 7, MsgAudio, strid, tsdelta, d)
}

func (mr *MsgStream) WriteAAC(strid, ts int, data []byte) {
	d := append([]byte{0xaf, 0}, data...)
	mr.ats = ts
	mr.WriteMsg(0, 7, MsgAudio, strid, ts, d)
}

func (mr *MsgStream) WriteVideo(strid, ts int, key bool, data []byte) {
	var b int
	if key {
		b = 0x17
	} else {
		b = 0x27
	}
	d := append([]byte{byte(b), 1, 0, 0, 0x50}, data...)
	tsdelta := ts - mr.vts
	mr.vts = ts
	mr.WriteMsg(1, 6, MsgVideo, strid, tsdelta, d)
}

func (mr *MsgStream) WritePPS(strid, ts int, data []byte) {
	d := append([]byte{0x17, 0, 0, 0, 0}, data...)
	mr.vts = ts
	mr.WriteMsg(0, 6, MsgVideo, strid, ts, d)
}

func (mr *MsgStream) WriteAMFMeta(csid, strid int, a []AMFObj) {
	var b bytes.Buffer
	for _, v := range a {
		WriteAMF(&b, v)
	}
	mr.WriteMsg(0, csid, MsgAmfMeta, strid, 0, b.Bytes())
}

func (mr *MsgStream) WriteAMFCmd(csid, strid int, a []AMFObj) {
	var b bytes.Buffer
	for _, v := range a {
		WriteAMF(&b, v)
	}
	mr.WriteMsg(0, csid, MsgAmfCmd, strid, 0, b.Bytes())
}

func (mr *MsgStream) WriteMsg32(csid, typeid, strid, v int) {
	var b bytes.Buffer
	WriteInt(&b, v, 4)
	mr.WriteMsg(0, csid, typeid, strid, 0, b.Bytes())
}

func (mr *MsgStream) ReadMsg() *Msg {
	ch := readChunkHeader(mr.r)
	m, ok := mr.Msg[ch.csId]
	if !ok {
		//l.Printf("chunk:   new")
		m = &Msg{ch, &bytes.Buffer{}, false, 0}
		mr.Msg[ch.csId] = m
	}

	switch ch.cFmt {
	case 0:
		m.ts = ch.ts
		m.mLen = ch.mLen
		m.typeId = ch.typeId
		m.curts = m.ts
	case 1:
		m.tsDelta = ch.tsDelta
		m.mLen = ch.mLen
		m.typeId = ch.typeId
		m.curts += m.tsDelta
	case 2:
		m.tsDelta = ch.tsDelta
	}

	left := m.mLen - m.data.Len()
	size := 128
	if size > left {
		size = left
	}
	//l.Printf("chunk:   %v", m)
	if size > 0 {
		_, _ = io.CopyN(m.data, mr.r, int64(size))
	}

	if size == left {
		rm := new(Msg)
		*rm = *m
		l.Printf("event: fmt%d %v curts %d pre %v", ch.cFmt, m, m.curts, m.data.Bytes()[:9])
		if m.typeId == MsgVideo && int(m.data.Bytes()[0]) == 0x17 {
			rm.key = true
		} else {
			rm.key = false
		}
		m.data = &bytes.Buffer{}
		return rm
	}

	return nil
}
