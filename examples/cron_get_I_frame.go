package main

import (
	"errors"
	"fmt"
	"github.com/peace0phmind/gmf"
	"github.com/robfig/cron/v3"
	"io"
	"log"
	"syscall"
	"time"
)

type myJob struct {
	CameraIndexCode string
	Url string
	Interval int
	entityId cron.EntryID
}


func assert(i interface{}, err error) interface{} {
	if err != nil {
		log.Fatal(err)
	}

	return i
}

func addStream(codecNameOrId interface{}, oc *gmf.FmtCtx, ist *gmf.Stream) (int, *gmf.Stream) {
	var cc *gmf.CodecCtx
	var ost *gmf.Stream

	codec := assert(gmf.FindEncoder(codecNameOrId)).(*gmf.Codec)

	if cc = gmf.NewCodecCtx(codec); cc == nil {
		log.Fatal(errors.New("unable to create codec context"))
	}

	if cc.Type() == gmf.AVMEDIA_TYPE_AUDIO {
		//cc.SetSampleFmt(ist.CodecCtx().SampleFmt())
		//cc.SetSampleRate(ist.CodecCtx().SampleRate())
		//cc.SetChannels(ist.CodecCtx().Channels())
		//cc.SelectChannelLayout()
		//cc.SelectSampleRate()
	}

	if cc.Type() == gmf.AVMEDIA_TYPE_VIDEO {
		cc.SetDimension(1280, 720)
		cc.SetBitRate(1000)
	}

	if ost = oc.NewStream(codec); ost == nil {
		log.Fatal(errors.New("unable to create stream in output context"))
	}

	ost.SetCodecCtx(cc)

	return ist.Index(), ost
}

func (job *myJob) Run() {
	log.Printf("Run Job %s, url: %s", job.CameraIndexCode, job.Url)

	inputOptionsDict := gmf.NewDict([]gmf.Pair{{Key: "rtsp_transport", Val: "tcp"}, {Key: "stimeout", Val: "10000000"}})
	inputOption := &gmf.Option{Key: "input_options", Val: inputOptionsDict}
	inputCtx, err := gmf.NewInputCtxWithOption(job.Url, inputOption)
	if err != nil {
		log.Fatal(err)
	}
	defer inputCtx.Free()
	inputCtx.Dump()

	outputCtx, err := gmf.NewOutputCtx(fmt.Sprintf("%s_%d.jpeg", job.CameraIndexCode, time.Now().Unix()))
	if err != nil {
		log.Fatal(err)
	}
	defer outputCtx.Free()
	outputCtx.Dump()

	bestVideoStream := assert(inputCtx.GetBestStream(gmf.AVMEDIA_TYPE_VIDEO)).(*gmf.Stream)
	_, outVideoStream := addStream("mjpeg", outputCtx, bestVideoStream)

	videoFg, err := gmf.NewVideoGraph("select='eq(pict_type\\,I)'", []*gmf.Stream{bestVideoStream}, []*gmf.Stream{outVideoStream}, nil)
	defer videoFg.Release()

	if err != nil {
		log.Fatalf("%s\n", err)
	}

	init := false

	var (
		ret   int = 0
		frame *gmf.Frame
		ff    []*gmf.Frame
		pkt   *gmf.Packet
		ist   *gmf.Stream
		ost   *gmf.Stream
		finished bool
	)

	finished = false

	for !finished {
		pkt, err = inputCtx.GetNextPacket()
		if err != nil && err != io.EOF {
			log.Fatalf("error getting next packet - %s", err)
		} else if err != nil && pkt == nil {
			log.Printf("EOF input, closing\n")
			videoFg.RequestOldest()
			videoFg.Close(0)

			continue
		}

		ist, err = inputCtx.GetStream(pkt.StreamIndex())
		if err != nil {
			log.Fatalf("%s\n", err)
		}

		var fg *gmf.FilterGraph
		if ist.IsVideo() {
			fg = videoFg
		} else {
			continue
		}

		frame, ret = ist.CodecCtx().Decode2(pkt)
		if ret < 0 && gmf.AvErrno(ret) == syscall.EAGAIN {
			continue
		} else if ret == gmf.AVERROR_EOF {
			log.Fatalf("EOF in Decode2, handle it\n")
		} else if ret < 0 {
			log.Fatalf("Unexpected error - %s\n", gmf.AvError(ret))
		}

		if frame != nil && !init {
			if err := fg.AddFrame(frame, 0, 0); err != nil {
				log.Fatalf("%s\n", err)
			}
			fg.Dump()

			if err := outputCtx.WriteHeader(); err != nil {
				log.Fatalf("error writing header - %s\n", err)
			}

			init = true
			frame.Free()
			continue
		}

		if err := fg.AddFrame(frame, 0, 4); err != nil {
			log.Fatalf("%s\n", err)
		}
		frame.Free()

		if ff, err = fg.GetFrame(); err != nil && len(ff) == 0 {
			//log.Printf("GetFrame() returned '%s', continue\n", err)
			continue
		}

		if len(ff) == 0 {
			continue
		}

		ost = assert(outputCtx.GetStream(pkt.StreamIndex())).(*gmf.Stream)
		packets, err := ost.CodecCtx().Encode(ff, -1)
		if err != nil {
			log.Fatalf("%s\n", err)
		}

		for _, f := range ff {
			f.Free()
		}

		for _, op := range packets {
			//gmf.RescaleTs(op, ost.CodecCtx().TimeBase(), ost.TimeBase())
			op.SetStreamIndex(ost.Index())

			op.Data()
			if err = outputCtx.WritePacket(op); err != nil {
				break
			}

			finished = true
			op.Free()
		}
	}

	outputCtx.WriteTrailer()

	ost.CodecCtx().Free()
	ost.Free()

	log.Printf("Job finished %s, url: %s", job.CameraIndexCode, job.Url)
}

func NewJob(cameraCode string, url string, interval int) *myJob {
	return &myJob{CameraIndexCode: cameraCode, Url: url, Interval: interval}
}

func AddJob(c *cron.Cron, j *myJob) error {
	if id, err := c.AddJob(fmt.Sprintf("*/%d * * * * *", j.Interval), j); err != nil {
		errors.New(fmt.Sprintf("Add job error: %v", err))
		return err
	} else {
		j.entityId = id
		return nil
	}
}

func main() {
	job1 := NewJob("101", "rtsp://admin:Zyx123456@192.168.1.10", 5)
	job2 := NewJob("102", "rtsp://admin:Zyx123456@192.168.1.11", 5)

	c := cron.New(cron.WithSeconds())
	AddJob(c, job1)
	AddJob(c, job2)
	c.Start()

	time.Sleep(4*time.Hour)
	c.Stop()
}