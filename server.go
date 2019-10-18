package rtmpgo

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"reflect"
	_ "runtime/debug"
	"strings"
	"time"
)

var (
	event     = make(chan eventS, 0)
	eventDone = make(chan int, 0)
)

type eventS struct {
	id int
	mr *MsgStream
	m  *Msg
}

type eventID int

func (e eventS) String() string {
	switch e.id {
	case ENew:
		return "new"
	case EPublish:
		return "publish"
	case EPlay:
		return "play"
	case EData:
		switch e.m.typeId {
		case MsgVideo:
			return fmt.Sprintf("ts %d video %d bytes key %t", e.m.curts, e.m.data.Len(), e.m.key)
		case MsgAudio:
			return fmt.Sprintf("ts %d audio %d bytes", e.m.curts, e.m.data.Len())
		}
	case EClose:
		return "close"
	}
	return ""
}

const (
	ENew = iota
	EPublish
	EPlay
	EData
	EClose
)

func handleConnect(mr *MsgStream, trans float64, app string) {

	l.Printf("stream %v: connect: %s", mr, app)

	mr.app = app

	mr.WriteMsg32(2, MsgAckSize, 0, 5000000)
	mr.WriteMsg32(2, MsgBandwidth, 0, 5000000)
	mr.WriteMsg32(2, MsgChunkSize, 0, 128)

	mr.WriteAMFCmd(3, 0, []AMFObj{
		{aType: AmfString, str: "_result"},
		{aType: AmfNumber, f64: trans},
		{aType: AmfObject,
			obj: map[string]AMFObj{
				"fmtVer":       {aType: AmfString, str: "FMS/3,0,1,123"},
				"capabilities": {aType: AmfNumber, f64: 31},
			},
		},
		{aType: AmfObject,
			obj: map[string]AMFObj{
				"level":          {aType: AmfString, str: "status"},
				"code":           {aType: AmfString, str: "NetConnection.Connect.Success"},
				"description":    {aType: AmfString, str: "Connection Success."},
				"objectEncoding": {aType: AmfNumber, f64: 0},
			},
		},
	})
}

func handleMeta(mr *MsgStream, obj AMFObj) {

	mr.meta = obj
	mr.W = int(obj.obj["width"].f64)
	mr.H = int(obj.obj["height"].f64)

	l.Printf("stream %v: meta video %dx%d", mr, mr.W, mr.H)
}

func handleCreateStream(mr *MsgStream, trans float64) {

	l.Printf("stream %v: createStream", mr)

	mr.WriteAMFCmd(3, 0, []AMFObj{
		{aType: AmfString, str: "_result"},
		{aType: AmfNumber, f64: trans},
		{aType: AmfNull},
		{aType: AmfNumber, f64: 1},
	})
}

func handleGetStreamLength(mr *MsgStream, trans float64) {
}

func handlePublish(mr *MsgStream) {

	l.Printf("stream %v: publish", mr)

	mr.WriteAMFCmd(3, 0, []AMFObj{
		{aType: AmfString, str: "onStatus"},
		{aType: AmfNumber, f64: 0},
		{aType: AmfNull},
		{aType: AmfObject,
			obj: map[string]AMFObj{
				"level":       {aType: AmfString, str: "status"},
				"code":        {aType: AmfString, str: "NetStream.Publish.Start"},
				"description": {aType: AmfString, str: "Start publising."},
			},
		},
	})

	event <- eventS{id: EPublish, mr: mr}
	<-eventDone
}

type testSrc struct {
	r     *bufio.Reader
	dir   string
	w, h  int
	ts    int
	codec string
	key   bool
	idx   int
	data  []byte
}

func tSrcNew() (m *testSrc) {
	m = &testSrc{}
	m.dir = "/pixies/go/data/tmp"
	fi, _ := os.Open(fmt.Sprintf("%s/index", m.dir))
	m.r = bufio.NewReader(fi)
	l, _ := m.r.ReadString('\n')
	_, _ = fmt.Sscanf(l, "%dx%d", &m.w, &m.h)
	return
}

func (m *testSrc) fetch() (err error) {
	l, err := m.r.ReadString('\n')
	if err != nil {
		return
	}
	a := strings.Split(l, ",")
	_, _ = fmt.Sscanf(a[0], "%d", &m.ts)
	m.codec = a[1]
	_, _ = fmt.Sscanf(a[2], "%d", &m.idx)
	switch m.codec {
	case "h264":
		_, _ = fmt.Sscanf(a[3], "%t", &m.key)
		m.data, err = ioutil.ReadFile(fmt.Sprintf("%s/h264/%d.264", m.dir, m.idx))
	case "aac":
		m.data, err = ioutil.ReadFile(fmt.Sprintf("%s/aac/%d.aac", m.dir, m.idx))
	}
	return
}

func handlePlay(mr *MsgStream, strid int) {

	l.Printf("stream %v: play", mr)

	var tsrc *testSrc
	//tsrc = tSrcNew()

	if tsrc == nil {
		event <- eventS{id: EPlay, mr: mr}
		<-eventDone
	} else {
		l.Printf("stream %v: test play data in %s", mr, tsrc.dir)
		mr.W = tsrc.w
		mr.H = tsrc.h
		l.Printf("stream %v: test video %dx%d", mr, mr.W, mr.H)
	}

	begin := func() {

		var b bytes.Buffer
		WriteInt(&b, 0, 2)
		WriteInt(&b, strid, 4)
		mr.WriteMsg(0, 2, MsgUser, 0, 0, b.Bytes()) // stream begin 1

		mr.WriteAMFCmd(5, strid, []AMFObj{
			{aType: AmfString, str: "onStatus"},
			{aType: AmfNumber, f64: 0},
			{aType: AmfNull},
			{aType: AmfObject,
				obj: map[string]AMFObj{
					"level":       {aType: AmfString, str: "status"},
					"code":        {aType: AmfString, str: "NetStream.Play.Start"},
					"description": {aType: AmfString, str: "Start live."},
				},
			},
		})

		l.Printf("stream %v: begin: video %dx%d", mr, mr.W, mr.H)

		mr.WriteAMFMeta(5, strid, []AMFObj{
			{aType: AmfString, str: "|RtmpSampleAccess"},
			{aType: AmfBoolean, i: 1},
			{aType: AmfBoolean, i: 1},
		})

		mr.meta.obj["Server"] = AMFObj{aType: AmfString, str: "Golang RTMP Server"}
		mr.meta.aType = AmfObject
		l.Printf("stream %v: %v", mr, mr.meta)
		mr.WriteAMFMeta(5, strid, []AMFObj{
			{aType: AmfString, str: "onMetaData"},
			mr.meta,
			/*
				AMFObj { aType : AMF_OBJECT,
					obj : map[string] AMFObj {
						"Server" : AMFObj { aType : AMF_STRING, str : "Golang Rtmp Server", },
						"width" : AMFObj { aType : AMF_NUMBER, f64 : float64(mr.W), },
						"height" : AMFObj { aType : AMF_NUMBER, f64 : float64(mr.H), },
						"displayWidth" : AMFObj { aType : AMF_NUMBER, f64 : float64(mr.W), },
						"displayHeight" : AMFObj { aType : AMF_NUMBER, f64 : float64(mr.H), },
						"duration" : AMFObj { aType : AMF_NUMBER, f64 : 0, },
						"framerate" : AMFObj { aType : AMF_NUMBER, f64 : 25000, },
						"videodatarate" : AMFObj { aType : AMF_NUMBER, f64 : 731, },
						"videocodecid" : AMFObj { aType : AMF_NUMBER, f64 : 7, },
						"audiodatarate" : AMFObj { aType : AMF_NUMBER, f64 : 122, },
						"audiocodecid" : AMFObj { aType : AMF_NUMBER, f64 : 10, },
					},
				},
			*/
		})
	}

	end := func() {

		l.Printf("stream %v: end", mr)

		var b bytes.Buffer
		WriteInt(&b, 1, 2)
		WriteInt(&b, strid, 4)
		mr.WriteMsg(0, 2, MsgUser, 0, 0, b.Bytes()) // stream eof 1

		mr.WriteAMFCmd(5, strid, []AMFObj{
			{aType: AmfString, str: "onStatus"},
			{aType: AmfNumber, f64: 0},
			{aType: AmfNull},
			{aType: AmfObject,
				obj: map[string]AMFObj{
					"level":       {aType: AmfString, str: "status"},
					"code":        {aType: AmfString, str: "NetStream.Play.Stop"},
					"description": {aType: AmfString, str: "Stop live."},
				},
			},
		})
	}

	if tsrc == nil {

		for {
			nr := 0

			for {
				m := <-mr.que
				if m == nil {
					break
				}
				//if nr == 0 && !m.key {
				//	continue
				//}
				if nr == 0 {
					begin()
					l.Printf("stream %v: extra size %d %d", mr, len(mr.extraA), len(mr.extraV))
					mr.WriteAAC(strid, 0, mr.extraA[2:])
					mr.WritePPS(strid, 0, mr.extraV[5:])
				}
				l.Printf("data %v: got %v curts %v", mr, m, m.curts)
				switch m.typeId {
				case MsgAudio:
					mr.WriteAudio(strid, m.curts, m.data.Bytes()[2:])
				case MsgVideo:
					mr.WriteVideo(strid, m.curts, m.key, m.data.Bytes()[5:])
				}
				nr++
			}
			end()
		}

	} else {

		begin()

		lf, _ := os.Create("/tmp/rtmp.log")
		ll := log.New(lf, "", 0)

		starttm := time.Now()
		k := 0

		for {
			err := tsrc.fetch()
			if err != nil {
				panic(err)
			}
			switch tsrc.codec {
			case "h264":
				if tsrc.idx == 0 {
					mr.WritePPS(strid, 0, tsrc.data)
				} else {
					mr.WriteVideo(strid, tsrc.ts, tsrc.key, tsrc.data)
				}
			case "aac":
				if tsrc.idx == 0 {
					mr.WriteAAC(strid, 0, tsrc.data)
				} else {
					mr.WriteAudio(strid, tsrc.ts, tsrc.data)
				}
			}
			dur := time.Since(starttm).Nanoseconds()
			diff := tsrc.ts - 1000 - int(dur/1000000)
			if diff > 0 {
				time.Sleep(time.Duration(diff) * time.Millisecond)
			}
			l.Printf("data %v: ts %v dur %v diff %v", mr, tsrc.ts, int(dur/1000000), diff)
			ll.Printf("#%d %d,%s,%d %d", k, tsrc.ts, tsrc.codec, tsrc.idx, len(tsrc.data))
			k++
		}
	}
}

func serve(mr *MsgStream) {

	defer func() {
		if err := recover(); err != nil {
			event <- eventS{id: EClose, mr: mr}
			<-eventDone
			l.Printf("stream %v: closed %v", mr, err)
			//if err != "EOF" {
			//	l.Printf("stream %v: %v", mr, string(debug.Stack()))
			//}
		}
	}()

	handShake(mr.r)

	//	f, _ := os.Create("/tmp/pub.log")
	//	mr.l = log.New(f, "", 0)

	for {
		m := mr.ReadMsg()
		if m == nil {
			continue
		}

		//l.Printf("stream %v: msg %v", mr, m)

		if m.typeId == MsgAudio || m.typeId == MsgVideo {
			//			mr.l.Printf("%d,%d", m.typeId, m.data.Len())
			event <- eventS{id: EData, mr: mr, m: m}
			<-eventDone
		}

		if m.typeId == MsgAmfCmd || m.typeId == MsgAmfMeta {
			a := ReadAMF(m.data)
			//l.Printf("server: amfobj %v\n", a)
			switch a.str {
			case "connect":
				a2 := ReadAMF(m.data)
				a3 := ReadAMF(m.data)
				if _, ok := a3.obj["app"]; !ok || a3.obj["app"].str == "" {
					panic("connect: app not found")
				}
				handleConnect(mr, a2.f64, a3.obj["app"].str)
			case "@setDataFrame":
				ReadAMF(m.data)
				a3 := ReadAMF(m.data)
				handleMeta(mr, a3)
				l.Printf("stream %v: setdataframe", mr)
			case "createStream":
				a2 := ReadAMF(m.data)
				handleCreateStream(mr, a2.f64)
			case "publish":
				handlePublish(mr)
			case "play":
				handlePlay(mr, m.strId)
			}
		}
	}
}

func listenEvent() {
	idmap := map[string]*MsgStream{}
	pubmap := map[string]*MsgStream{}

	for {
		e := <-event
		if e.id == EData {
			l.Printf("data %v: %v", e.mr, e)
		} else {
			l.Printf("event %v: %v", e.mr, e)
		}
		switch {
		case e.id == ENew:
			idmap[e.mr.id] = e.mr
		case e.id == EPublish:
			if _, ok := pubmap[e.mr.app]; ok {
				l.Printf("event %v: duplicated publish with %v app %s", e.mr, pubmap[e.mr.app], e.mr.app)
				e.mr.Close()
			} else {
				e.mr.role = PUBLISHER
				pubmap[e.mr.app] = e.mr
			}
		case e.id == EPlay:
			e.mr.role = PLAYER
			e.mr.que = make(chan *Msg, 16)
			for _, mr := range idmap {
				if mr.role == PUBLISHER && mr.app == e.mr.app && mr.stat == WaitData {
					e.mr.W = mr.W
					e.mr.H = mr.H
					e.mr.extraA = mr.extraA
					e.mr.extraV = mr.extraV
					e.mr.meta = mr.meta
				}
			}
		case e.id == EClose:
			if e.mr.role == PUBLISHER {
				delete(pubmap, e.mr.app)
				for _, mr := range idmap {
					if mr.role == PLAYER && mr.app == e.mr.app {
						ch := reflect.ValueOf(mr.que)
						var m *Msg = nil
						ch.TrySend(reflect.ValueOf(m))
					}
				}
			}
			delete(idmap, e.mr.id)
		case e.id == EData && e.mr.stat == WaitExtra:
			if len(e.mr.extraA) == 0 && e.m.typeId == MsgAudio {
				l.Printf("event %v: got aac config", e.mr)
				e.mr.extraA = e.m.data.Bytes()
			}
			if len(e.mr.extraV) == 0 && e.m.typeId == MsgVideo {
				l.Printf("event %v: got pps", e.mr)
				e.mr.extraV = e.m.data.Bytes()
			}
			if len(e.mr.extraA) > 0 && len(e.mr.extraV) > 0 {
				l.Printf("event %v: got all extra", e.mr)
				e.mr.stat = WaitData
				for _, mr := range idmap {
					if mr.role == PLAYER && mr.app == e.mr.app {
						mr.W = e.mr.W
						mr.H = e.mr.H
						mr.extraA = e.mr.extraA
						mr.extraV = e.mr.extraV
						mr.meta = e.mr.meta
					}
				}
			}
		case e.id == EData && e.mr.stat == WaitData:
			for _, mr := range idmap {
				if mr.role == PLAYER && mr.app == e.mr.app {
					ch := reflect.ValueOf(mr.que)
					ok := ch.TrySend(reflect.ValueOf(e.m))
					if !ok {
						l.Printf("event %v: send failed", e.mr)
					} else {
						l.Printf("event %v: send ok", e.mr)
					}
				}
			}
		}
		eventDone <- 1
	}
}

func Serve(addr string) {
	l.Printf("server: simple server starts on %s\n", addr)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		l.Printf("server: error: listen %s %s\n", addr, err)
		return
	}
	go listenEvent()
	for {
		c, err := ln.Accept()
		if err != nil {
			l.Printf("server: error: sock accept %s\n", err)
			break
		}
		go func(c net.Conn) {
			mr := NewMsgStream(c)
			event <- eventS{id: ENew, mr: mr}
			<-eventDone
			serve(mr)
		}(c)
	}
}
