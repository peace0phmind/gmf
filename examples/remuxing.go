package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime/debug"

	. "github.com/peace0phmind/gmf"
)

func fatal(err error) {
	debug.PrintStack()
	log.Fatal(err)
}

func assert(i interface{}, err error) interface{} {
	if err != nil {
		fatal(err)
	}

	return i
}

func main() {
	var srcFileName, dstFileName string
	//LogSetLevel(AV_LOG_DEBUG)

	log.SetFlags(log.Lshortfile | log.Ldate | log.Ltime)

	flag.StringVar(&srcFileName, "src", "rtsp://admin:Zyx123456@192.168.1.10", "source file")
	flag.StringVar(&dstFileName, "dst", "http://121.36.218.177:9081/uVMChBVGg1", "destination file")
	flag.Parse()

	if len(srcFileName) == 0 || dstFileName == "" {
		fmt.Println("usage:", os.Args[0], " input output")
		fmt.Println("API example program to remux a media file with libavformat and libavcodec.")
		fmt.Println("The output format is guessed according to the file extension.")
		os.Exit(0)
	}

	inputOptionsDict := NewDict([]Pair{{Key: "rtsp_transport", Val: "tcp"}, {Key: "stimeout", Val: "10000000"}})
	inputOption := &Option{Key: "input_options", Val: inputOptionsDict}
	inputCtx := assert(NewInputCtxWithOption(srcFileName, inputOption)).(*FmtCtx)
	defer inputCtx.Free()
	inputCtx.Dump()

	outputCtx := assert(NewOutputCtxWithFormatName(dstFileName, "mpegts")).(*FmtCtx)
	defer outputCtx.Free()

	fmt.Println("===================================")

	for i := 0; i < inputCtx.StreamsCnt(); i++ {
		srcStream, err := inputCtx.GetStream(i)
		if err != nil {
			fmt.Println("GetStream error")
		}

		outputCtx.AddStreamWithCodeCtx(srcStream.CodecCtx())
	}
	outputCtx.Dump()

	if err := outputCtx.WriteHeader(); err != nil {
		fatal(err)
	}

	first := false
	for packet := range inputCtx.GetNewPackets() {

		if first { //if read from rtsp ,the first packets is wrong.
			if err := outputCtx.WritePacket(packet); err != nil {
				fatal(err)
			}
		}

		first = true
		packet.Free()
	}

}
