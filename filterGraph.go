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
        return ret;
    } else {
        return NULL;
	}
}

*/
import "C"

import (
	"fmt"
	"strings"
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
	var avFilters = strings.TrimSpace(desc)

	if len(avFilters) == 0 {
		avFilters = "null"
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
		video:         true,
		inStreams:     inStreams,
		outStreams:    outStreams,
		options:       options,
	}

	return f, nil
}

func (fg *FilterGraph) configureVideo(frame *Frame) error {
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

	if ret = int(C.avfilter_graph_parse2(
		fg.filterGraph,
		fg.avFilters,
		&inputs,
		&outputs,
	)); ret < 0 {
		return fmt.Errorf("error parsing filter graph - %s", AvError(ret))
	}
	defer C.avfilter_inout_free(&inputs)
	defer C.avfilter_inout_free(&outputs)

	i = 0
	for cur := inputs; cur != nil; cur = cur.next {
		fg.configVideoInput(frame, i, cur)
		i++
	}

	i = 0
	for cur := outputs; cur != nil; cur = cur.next {
		fg.configVideoOutput(i, cur)
		i++
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

	if ret = int(C.avfilter_link(filterContext, 0, in.filter_ctx, in.pad_idx)); ret < 0 {
		return fmt.Errorf("error linking filters - %s", AvError(ret))
	}

	return nil
}

func (fg *FilterGraph) configVideoOutput(idx int, out *C.AVFilterInOut) error {

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

	var args = fmt.Sprintf("%d:%d:flags=bicubic", occ.Width(), occ.Height())
	if scaleContext, ret = fg.create("scale", fmt.Sprintf("scale_%d", idx), args); ret < 0 {
		return fmt.Errorf("error creating filter 'scale' - %s", AvError(ret))
	}

	if ret = int(C.avfilter_link(lastFilterContext, padIdx, scaleContext, 0)); ret < 0 {
		return fmt.Errorf("error linking filters - %s", AvError(ret))
	}

	lastFilterContext = scaleContext
	padIdx = 0

	/****************************** format ******************************/
	if pixName := C.gmf_choose_pix_fmts(occ.codec.avCodec); pixName != nil {
		defer C.av_freep(unsafe.Pointer(pixName))

		if formatContext, ret = fg.create("format", fmt.Sprintf("format_%d", idx), C.GoString(pixName)); ret < 0 {
			return fmt.Errorf("error creating filter 'format' - %s", AvError(ret))
		}

		if ret = int(C.avfilter_link(lastFilterContext, padIdx, formatContext, 0)); ret < 0 {
			return fmt.Errorf("error linking filters - %s", AvError(ret))
		}

		lastFilterContext = formatContext
		padIdx = 0
	}

	/****************************** link last to sink ******************************/
	if ret = int(C.avfilter_link(lastFilterContext, padIdx, sinkContext, 0)); ret < 0 {
		return fmt.Errorf("error linking filters - %s", AvError(ret))
	}

	return nil
}

func (fg *FilterGraph) create(filter, name, args string) (*C.AVFilterContext, int) {
	var (
		ctx *C.AVFilterContext
		ret int
	)

	cfilter := C.CString(filter)
	cname := C.CString(name)

	var cargs C.CString
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
