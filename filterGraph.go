// +build go1.12

package gmf

/*
#cgo pkg-config: libavfilter libavutil

#include <stdio.h>
#include <libavformat/avformat.h>
#include <libavfilter/buffersink.h>
#include <libavfilter/buffersrc.h>
#include <libavutil/pixdesc.h>
#include <libavutil/display.h>

double gmf_get_rotation(AVStream *st)
{
    uint8_t* displaymatrix = av_stream_get_side_data(st,
                                                     AV_PKT_DATA_DISPLAYMATRIX, NULL);
    double theta = 0;
    if (displaymatrix)
        theta = -av_display_rotation_get((int32_t*) displaymatrix);

    theta -= 360*floor(theta/360 + 0.9/360);

    if (fabs(theta - 90*round(theta/90)) > 2)
        av_log(NULL, AV_LOG_WARNING, "Odd rotation angle.\n"
               "If you want to help, upload a sample "
               "of this file to https://streams.videolan.org/upload/ "
               "and contact the ffmpeg-devel mailing list. (ffmpeg-devel@ffmpeg.org)");

    return theta;
}

static char *gmf_choose_pix_fmts(AVCodec *enc)
{
    if (enc && enc->pix_fmts) {
        const enum AVPixelFormat *p;
        AVIOContext *s = NULL;
        uint8_t *ret;
        int len;

        if (avio_open_dyn_buf(&s) < 0) {
			return NULL;
		}

        p = enc->pix_fmts;

        for (; *p != AV_PIX_FMT_NONE; p++) {
            const char *name = av_get_pix_fmt_name(*p);
            avio_printf(s, "%s|", name);
        }

        len = avio_close_dyn_buf(s, &ret);
        ret[len - 1] = 0;
        return (char*)ret;
    } else {
        return NULL;
	}
}

static void gmf_set_nearest_framerate(AVCodecContext *cc, AVCodec *c) {
	int idx = av_find_nearest_q_idx(cc->framerate, c->supported_framerates);
	cc->framerate = c->supported_framerates[idx];
}

static void gmf_set_pix_fmt_from_sink(AVCodecContext *cc, AVFilterContext *sinkFilterContext) {
	cc->pix_fmt = av_buffersink_get_format(sinkFilterContext);
}
*/
import "C"

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"syscall"
	"unsafe"
)

type FilterGraph struct {
	inStreams     []*Stream
	outStreams    []*Stream
	inFilterCtxs  []*C.AVFilterContext
	outFilterCtxs []*C.AVFilterContext
	formatCtx     *C.AVFilterContext
	filterGraph   *C.AVFilterGraph
	avFilters     string //filter desc string
	options       []*Option
	video         bool
}

func NewVideoGraph(desc string, inStreams []*Stream, outStreams []*Stream, options []*Option) (*FilterGraph, error) {
	return NewGraph(desc, AVMEDIA_TYPE_VIDEO, inStreams, outStreams, options)
}

func NewAudioGraph(desc string, inStreams []*Stream, outStreams []*Stream, options []*Option) (*FilterGraph, error) {
	return NewGraph(desc, AVMEDIA_TYPE_AUDIO, inStreams, outStreams, options)
}

func NewGraph(desc string, typ int32, inStreams []*Stream, outStreams []*Stream, options []*Option) (*FilterGraph, error) {
	if typ != AVMEDIA_TYPE_VIDEO && typ != AVMEDIA_TYPE_AUDIO {
		return nil, errors.New("Only support video and audio filter graph.")
	}

	var avFilters = strings.TrimSpace(desc)
	var video = typ == AVMEDIA_TYPE_VIDEO

	if len(avFilters) == 0 {
		if video {
			avFilters = "null"
		} else {
			avFilters = "anull"
		}
	} else {
		var (
			ret     int
			inputs  *C.AVFilterInOut
			outputs *C.AVFilterInOut
		)

		filterGraph := C.avfilter_graph_alloc()
		if filterGraph == nil {
			return nil, AvError(ENOMEM)
		}

		cdesc := C.CString(desc)
		defer C.free(unsafe.Pointer(cdesc))

		if ret = int(C.avfilter_graph_parse2(
			filterGraph,
			cdesc,
			&inputs,
			&outputs,
		)); ret < 0 {
			return nil, fmt.Errorf("error parsing filter graph: %s. error is: %s", desc, AvError(ret))
		}
		defer C.avfilter_graph_free(&filterGraph)
		defer C.avfilter_inout_free(&inputs)
		defer C.avfilter_inout_free(&outputs)
	}

	f := &FilterGraph{
		inFilterCtxs:  make([]*C.AVFilterContext, 0),
		outFilterCtxs: make([]*C.AVFilterContext, 0),
		avFilters:     avFilters,
		video:         video,
		inStreams:     inStreams,
		outStreams:    outStreams,
		options:       options,
	}

	return f, nil
}

func (fg *FilterGraph) configureGraph(frame *Frame) error {
	if fg.filterGraph == nil {
		fg.filterGraph = C.avfilter_graph_alloc()
		if fg.filterGraph == nil {
			return AvError(ENOMEM)
		}
	}

	// scale_sws_opts = "flags=bicubic"
	fg.filterGraph.scale_sws_opts = C.av_strdup(C.CString("flags=bicubic"))

	// aresample_swr_opts = ""
	Option{Key: "aresample_swr_opts", Val: ""}.Set(fg.filterGraph)

	var (
		ret, i  int
		inputs  *C.AVFilterInOut
		outputs *C.AVFilterInOut
	)

	cdesc := C.CString(fg.avFilters)
	defer C.free(unsafe.Pointer(cdesc))

	if ret = int(C.avfilter_graph_parse2(
		fg.filterGraph,
		cdesc,
		&inputs,
		&outputs,
	)); ret < 0 {
		return fmt.Errorf("error parsing filter graph - %s", AvError(ret))
	}
	defer C.avfilter_inout_free(&inputs)
	defer C.avfilter_inout_free(&outputs)

	i = 0
	for cur := inputs; cur != nil; cur = cur.next {
		if fg.video {
			fg.configVideoInput(frame, i, cur)
		} else {
			fg.configAudioInput(frame, i, cur)
		}
		i++
	}

	i = 0
	for cur := outputs; cur != nil; cur = cur.next {
		if fg.video {
			fg.configVideoOutput(frame, i, cur)
		} else {
			fg.configAudioOutput(frame, i, cur)
		}
		i++
	}

	if ret = int(C.avfilter_graph_config(fg.filterGraph, nil)); ret < 0 {
		return fmt.Errorf("graph config error - %s", AvError(ret))
	}

	return nil
}

func (fg *FilterGraph) initVideoEncoderContext(idx int) error {
	src := fg.inStreams[idx]
	dest := fg.outStreams[idx]

	if dest.CodecCtx().IsOpen() {
		return nil
	}

	dest.avStream.disposition = src.avStream.disposition

	encCtx := dest.CodecCtx()
	decCtx := src.CodecCtx()

	encCtx.avCodecCtx.chroma_sample_location = decCtx.avCodecCtx.chroma_sample_location

	/****************************** set frame rate ******************************/
	if encCtx.GetFrameRate().AVR().Num == 0 {
		encCtx.avCodecCtx.framerate = C.av_buffersink_get_frame_rate(fg.outFilterCtxs[0])
	}

	if encCtx.GetFrameRate().AVR().Num == 0 {
		encCtx.avCodecCtx.framerate = decCtx.avCodecCtx.framerate
	}

	if encCtx.GetFrameRate().AVR().Num == 0 {
		encCtx.avCodecCtx.framerate = src.avStream.r_frame_rate
	}

	if encCtx.GetFrameRate().AVR().Num == 0 {
		encCtx.avCodecCtx.framerate.num = 25
		encCtx.avCodecCtx.framerate.den = 1
		log.Println("No information about the input framerate is available. Falling back to a default value of 25fps")
	}

	if encCtx.codec.avCodec.supported_framerates != nil && !encCtx.forceFps {
		C.gmf_set_nearest_framerate(encCtx.avCodecCtx, encCtx.codec.avCodec)
	}

	/****************************** set time base ******************************/
	if encCtx.TimeBase().AVR().Num == 0 {
		encCtx.avCodecCtx.time_base = C.av_inv_q(encCtx.avCodecCtx.framerate)
	}

	sinkFilterContext := fg.outFilterCtxs[0]
	/****************************** set sample_aspect_ratio ******************************/
	encCtx.avCodecCtx.sample_aspect_ratio = C.av_buffersink_get_sample_aspect_ratio(sinkFilterContext)

	/****************************** set sample_aspect_ratio ******************************/
	C.gmf_set_pix_fmt_from_sink(encCtx.avCodecCtx, sinkFilterContext)

	/****************************** set bits_per_raw_sample ******************************/
	encCtx.avCodecCtx.bits_per_raw_sample = min(decCtx.avCodecCtx.bits_per_raw_sample,
		C.av_pix_fmt_desc_get(encCtx.avCodecCtx.pix_fmt).comp[0].depth)

	/****************************** set avg_frame_rate ******************************/
	dest.avStream.avg_frame_rate = encCtx.avCodecCtx.framerate

	encCtx.Open(nil)
	/****************************** set stream ******************************/
	if ret := C.avcodec_parameters_from_context(dest.avStream.codecpar, encCtx.avCodecCtx); ret < 0 {
		return errors.New("Error initializing the output stream codec context.")
	}

	return nil
}

func (fg *FilterGraph) configVideoInput(frame *Frame, idx int, in *C.AVFilterInOut) error {
	src := fg.inStreams[idx]
	tb := src.TimeBase()

	sr := frame.SampleAspectRatio().AVR()
	if sr.Den == 0 {
		sr = AVRational{0, 1}.AVR()
	}

	var ret int
	var args = fmt.Sprintf("video_size=%dx%d:pix_fmt=%d:time_base=%s:pixel_aspect=%s", frame.Width(),
		frame.Height(), frame.Format(), tb.AVR(), sr)

	fr := AVRational(C.av_guess_frame_rate(src.avFmtCtx.avCtx, src.avStream, nil)).AVR()
	if fr.Num != 0 && fr.Den != 0 {
		args += fmt.Sprintf(":frame_rate=%s", fr)
	}

	var filterContext *C.AVFilterContext
	if filterContext, ret = fg.create("buffer", fmt.Sprintf("in_%d", idx), args); ret < 0 {
		return fmt.Errorf("error creating input buffer - %s", AvError(ret))
	}

	fg.inFilterCtxs = append(fg.inFilterCtxs, filterContext)

	if ret = int(C.avfilter_link(filterContext, 0, in.filter_ctx, C.uint(in.pad_idx))); ret < 0 {
		return fmt.Errorf("error linking filters - %s", AvError(ret))
	}

	return nil
}

func (fg *FilterGraph) configAudioInput(frame *Frame, idx int, in *C.AVFilterInOut) error {
	src := fg.inStreams[idx]
	tb := src.TimeBase()

	sr := frame.SampleAspectRatio().AVR()
	if sr.Den == 0 {
		sr = AVRational{0, 1}.AVR()
	}

	var ret int
	var args = fmt.Sprintf("time_base=1/%d:sample_rate=%d:sample_fmt=%s", frame.SampleRate(), frame.SampleRate(),
		C.av_get_sample_fmt_name(frame.Format()))

	if frame.ChannelLayout() > 0 {
		args += fmt.Sprintf(":channel_layout=0x%d", frame.ChannelLayout())
	} else {
		args += fmt.Sprintf(":channels=%d", frame.Channels())
	}

	var filterContext *C.AVFilterContext
	if filterContext, ret = fg.create("abuffer", fmt.Sprintf("in_%d", idx), args); ret < 0 {
		return fmt.Errorf("error creating input abuffer - %s", AvError(ret))
	}

	fg.inFilterCtxs = append(fg.inFilterCtxs, filterContext)

	if ret = int(C.avfilter_link(filterContext, 0, in.filter_ctx, C.uint(in.pad_idx))); ret < 0 {
		return fmt.Errorf("error linking filters - %s", AvError(ret))
	}

	return nil
}

func (fg *FilterGraph) configVideoOutput(frame *Frame, idx int, out *C.AVFilterInOut) error {

	lastFilterContext := out.filter_ctx
	padIdx := out.pad_idx

	outStream := fg.outStreams[idx]

	var (
		ret           int
		sinkContext   *C.AVFilterContext
		scaleContext  *C.AVFilterContext
		formatContext *C.AVFilterContext
	)

	if sinkContext, ret = fg.create("buffersink", fmt.Sprintf("out_%d", idx), ""); ret < 0 {
		return fmt.Errorf("error creating filter 'buffersink' - %s", AvError(ret))
	}

	fg.outFilterCtxs = append(fg.outFilterCtxs, sinkContext)

	/****************************** auto scale ******************************/
	occ := outStream.CodecCtx()

	w := occ.Width()
	h := occ.Height()
	if w <= 0 || h <= 0 {
		w = frame.Width()
		h = frame.Height()
	}

	if !(w == frame.Width() && h == frame.Height()) {
		var args = fmt.Sprintf("%d:%d:flags=bicubic", w, h)
		if scaleContext, ret = fg.create("scale", fmt.Sprintf("scale_%d", idx), args); ret < 0 {
			return fmt.Errorf("error creating filter 'scale' - %s", AvError(ret))
		}

		if ret = int(C.avfilter_link(lastFilterContext, C.uint(padIdx), scaleContext, 0)); ret < 0 {
			return fmt.Errorf("error linking filters - %s", AvError(ret))
		}

		lastFilterContext = scaleContext
		padIdx = 0
	}

	/****************************** format ******************************/
	if pixFmtName := C.gmf_choose_pix_fmts(occ.codec.avCodec); pixFmtName != nil {
		defer C.av_freep(unsafe.Pointer(&pixFmtName))

		if formatContext, ret = fg.create("format", fmt.Sprintf("format_%d", idx), C.GoString(pixFmtName)); ret < 0 {
			return fmt.Errorf("error creating filter 'format' - %s", AvError(ret))
		}

		if ret = int(C.avfilter_link(lastFilterContext, C.uint(padIdx), formatContext, 0)); ret < 0 {
			return fmt.Errorf("error linking filters - %s", AvError(ret))
		}

		lastFilterContext = formatContext
		padIdx = 0
	}

	/****************************** link last to sink ******************************/
	if ret = int(C.avfilter_link(lastFilterContext, C.uint(padIdx), sinkContext, 0)); ret < 0 {
		return fmt.Errorf("error linking filters - %s", AvError(ret))
	}

	return nil
}

func (fg *FilterGraph) configAudioOutput(frame *Frame, idx int, out *C.AVFilterInOut) error {

	return nil
}

func (fg *FilterGraph) create(filter, name, args string) (*C.AVFilterContext, int) {
	var (
		ctx *C.AVFilterContext
		ret int
	)

	cfilter := C.CString(filter)
	cname := C.CString(name)

	var cargs *C.char
	if len(strings.TrimSpace(args)) == 0 {
		cargs = nil
	} else {
		cargs = C.CString(args)
	}

	ret = int(C.avfilter_graph_create_filter(
		&ctx,
		C.avfilter_get_by_name(cfilter),
		cname,
		cargs,
		nil,
		fg.filterGraph))

	C.free(unsafe.Pointer(cfilter))
	C.free(unsafe.Pointer(cname))

	if cargs != nil {
		C.free(unsafe.Pointer(cargs))
	}

	return ctx, ret
}

func (fg *FilterGraph) isVideo() bool {
	return fg.video
}

func (fg *FilterGraph) RequestOldest() error {
	if fg.filterGraph == nil {
		return errors.New("Graph not inited")
	}
	var ret int

	if ret = int(C.avfilter_graph_request_oldest(fg.filterGraph)); ret < 0 {
		return AvError(ret)
	}

	return nil
}

func (fg *FilterGraph) AddFrame(frame *Frame, istIdx int, flag int) error {
	var ret int

	if fg.filterGraph == nil {
		fg.configureGraph(frame)
	}

	if istIdx >= len(fg.inFilterCtxs) {
		return fmt.Errorf("unexpected stream index #%d", istIdx)
	}

	if ret = int(C.av_buffersrc_add_frame_flags(
		fg.inFilterCtxs[istIdx],
		frame.avFrame,
		C.int(flag)),
	); ret < 0 {
		return AvError(ret)
	}

	return nil
}

func (fg *FilterGraph) GetFrame() ([]*Frame, error) {

	var (
		ret    int
		result []*Frame = make([]*Frame, 0)
	)

	for {
		frame := NewFrame()

		ret = int(C.av_buffersink_get_frame_flags(fg.outFilterCtxs[0], frame.avFrame, AV_BUFFERSINK_FLAG_NO_REQUEST))
		if AvErrno(ret) == syscall.EAGAIN || ret == AVERROR_EOF {
			frame.Free()
			break
		} else if ret < 0 {
			frame.Free()
			return nil, AvError(ret)
		}

		result = append(result, frame)
	}

	fg.RequestOldest()

	if !fg.outStreams[0].CodecCtx().opened && fg.isVideo() {
		fg.initVideoEncoderContext(0)
	}

	return result, AvError(ret)
}

func (fg *FilterGraph) Close(istIdx int) error {
	var ret int

	if ret = int(C.av_buffersrc_close(fg.inFilterCtxs[istIdx], 0, AV_BUFFERSRC_FLAG_PUSH)); ret < 0 {
		return AvError(ret)
	}

	return nil
}

func (fg *FilterGraph) Dump() {
	if fg.filterGraph != nil {
		fmt.Println(C.GoString(C.avfilter_graph_dump(fg.filterGraph, nil)))
	} else {
		log.Println("Graph not inited when dump.")
	}
}

func (fg *FilterGraph) Release() {
	for _, inFilterCtx := range fg.inFilterCtxs {
		C.avfilter_free(inFilterCtx)
	}

	for _, out := range fg.outFilterCtxs {
		C.avfilter_free(out)
	}

	if fg.formatCtx != nil {
		C.avfilter_free(fg.formatCtx)
	}

	if fg.filterGraph != nil {
		C.avfilter_graph_free(&fg.filterGraph)
	}
}

func min(a, b C.int) C.int {
	if a < b {
		return a
	}
	return b
}
