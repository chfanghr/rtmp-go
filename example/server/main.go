package main

import rtmpgo "github.com/chfanghr/rtmp-go"

func main() {
	rtmpgo.Serve(":2000")
}
