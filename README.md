# rtmp-go

This is an **untested** implementation of RTMP server, written in golang.

## How does RTMP work
* Server side
    1. (media source) Connect to RTMP server
    2. Create stream
    3. Publish stream
* Client side
    1. Connect to RTMP server
    2. Create stream
    3. Get stream length
    4. Play the stream
