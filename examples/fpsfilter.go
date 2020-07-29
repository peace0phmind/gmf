package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"syscall"

	"github.com/peace0phmind/gmf"
)

const format = "./tmp/%03d.jpeg"

var fileCount int

func writeFile(b []byte) {
	name := fmt.Sprintf(format, fileCount)

	fp, err := os.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("%s\n", err)
	}

	if n, err := fp.Write(b); err != nil {
		log.Fatalf("%s\n", err)
	} else {
		log.Printf("%d bytes written to '%s'", n, name)
	}

	fp.Close()
	fileCount++
}

func main() {
	var (
		src string
		dst string
	)

	log.SetFlags(log.Lshortfile)
	//gmf.LogSetLevel(gmf.AV_LOG_DEBUG)
	os.MkdirAll("./tmp", 0755)

	flag.StringVar(&src, "src", "bbb.mp4", "source files, e.g.: -src=bbb.mp4 -src=image.png")
	flag.StringVar(&dst, "dst", "tt%d.png", "destination file, e.g. -dst=result.mp4")
	flag.Parse()

	if len(src) == 0 || dst == "" {
		log.Fatal("at least one source and destination required, e.g.\n./watermark -src=bbb.mp4 -src=test.png -dst=overlay.mp4")
	}

	ictx, err := gmf.NewInputCtx(src)
	if err != nil {
		log.Fatal(err)
	}
	defer ictx.Free()

	srcVideoStream, err := ictx.GetBestStream(gmf.AVMEDIA_TYPE_VIDEO)
	log.Printf("stream %s, %s\n", srcVideoStream.CodecCtx().Codec().LongName(), srcVideoStream.CodecCtx().GetVideoSize())

	srcCodec := srcVideoStream.CodecCtxWithCuda(true)
	defer gmf.Release(srcCodec)

	/************************/
	destCodec, err := gmf.FindEncoder("mjpeg")
	defer gmf.Release(destCodec)
	if err != nil {
		log.Fatal(err)
	}

	occ := gmf.NewCodecCtx(destCodec)
	defer gmf.Release(occ)

	occ.SetPixFmt(gmf.AV_PIX_FMT_YUVJ420P).SetWidth(srcVideoStream.CodecCtx().Width()).SetHeight(srcVideoStream.CodecCtx().Height())
	occ.SetTimeBase(gmf.AVR{Num: 1, Den: 1})

	if err := occ.Open(nil); err != nil {
		log.Fatal(err)
	}

	/*******************************/
	var (
		ret int = 0
		pkt *gmf.Packet
	)

	filter, err := gmf.NewFilter("fps=fps=1/5", []*gmf.Stream{srcVideoStream}, nil, nil)
	defer filter.Release()
	if err != nil {
		log.Fatalf("%s\n", err)
	}
	filter.Dump()

	var (
		frame *gmf.Frame
		ff    []*gmf.Frame
	)

	init := false

	for {
		pkt, err = ictx.GetNextPacket()
		if err != nil && err != io.EOF {
			log.Fatalf("error getting next packet - %s", err)
		} else if err != nil && (err == io.EOF || pkt == nil) {
			log.Printf("EOF input #%d, closing\n", 0)
			filter.RequestOldest()
			filter.Close(0)
			break
		}

		if pkt.StreamIndex() != srcVideoStream.Index() {
			continue
		}

		frame, ret = srcCodec.Decode2(pkt)
		if ret < 0 && gmf.AvErrno(ret) == syscall.EAGAIN {
			continue
		} else if ret == gmf.AVERROR_EOF {
			log.Fatalf("EOF in Decode2, handle it\n")
		} else if ret < 0 {
			log.Fatalf("Unexpected error - %s\n", gmf.AvError(ret))
		}

		if frame != nil && init != true {
			if err := filter.AddFrame(frame, 0, 0); err != nil {
				log.Fatalf("%s\n", err)
			}
			init = true
			frame.Free()
			continue
		}

		if err := filter.AddFrame(frame, 0, 4); err != nil {
			log.Fatalf("%s\n", err)
		}
		frame.Free()

		if ff, err = filter.GetFrame(); err != nil && len(ff) == 0 {
			//log.Printf("GetFrame() returned '%s', continue\n", err)
			continue
		}

		if len(ff) == 0 {
			continue
		}

		packets, err := occ.Encode(ff, -1)
		if err != nil {
			log.Fatal(err)
		}

		for _, f := range ff {
			f.Free()
		}

		for _, op := range packets {
			writeFile(op.Data())

			op.Free()
		}
	}

	for i := 0; i < ictx.StreamsCnt(); i++ {
		st, _ := ictx.GetStream(i)
		st.CodecCtx().Free()
		st.Free()
	}
}
