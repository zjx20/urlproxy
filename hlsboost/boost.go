package hlsboost

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/zjx20/urlproxy/ant"
	"github.com/zjx20/urlproxy/handler"
	"github.com/zjx20/urlproxy/logger"
	"github.com/zjx20/urlproxy/proxy"
	"github.com/zjx20/urlproxy/urlopts"

	"github.com/etherlabsio/go-m3u8/m3u8"
)

var (
	cacheDir = flag.String("cache-dir", "./cache", "Cache dir for HLS Boost")
)

func Handler(selfCli *SelfClient) handler.HttpHandler {
	return (&hlsBoost{
		selfCli: selfCli,
		mgr:     globalManager(),
	}).handle
}

type hlsBoost struct {
	selfCli *SelfClient
	mgr     *manager
}

func (h *hlsBoost) handle(w http.ResponseWriter, req *http.Request, opts *urlopts.Options) bool {
	if urlopts.ToVal(opts.HLSSkip, false) {
		// a request from SelfClient
		return false
	}
	if req.URL.Scheme != "" || req.Method != http.MethodGet {
		return false
	}
	if opts.HLSSegment != nil {
		return h.serveSegment(w, req, opts)
	}
	if opts.HLSBoost != nil {
		return h.servePlaylist(w, req, opts)
	}
	return false
}

func (h *hlsBoost) serveSegment(w http.ResponseWriter, req *http.Request, opts *urlopts.Options) bool {
	if opts.HLSPlaylist == nil || opts.HLSUser == nil || opts.HLSSegment == nil {
		// fallback to normal proxy
		return false
	}
	pl := h.mgr.GetPlaylist(*opts.HLSPlaylist)
	if pl == nil {
		logger.Warnf("playlist %s not found", *opts.HLSPlaylist)
		w.WriteHeader(http.StatusGone)
		return true
	}
	user := h.mgr.GetUser(*opts.HLSUser) // not nil
	seg := user.GetSegment(pl, *opts.HLSSegment)
	if seg == nil {
		// fallback to normal proxy
		return false
	}
	seg.Acquire()
	defer seg.Release()
	segSize, _ := seg.TotalSize(req.Context()) // blocking
	// status should be more reliable after getting total size
	if status := seg.Status(); status == ant.Aborted || status == ant.Destroyed {
		logger.Errorf("segment %s status (%d) is bad for serving", seg.segId, status)

		// segment prefetch failed, maybe the playlist is stale
		user.ResetProgress(pl.id)

		// fallback to the normal proxy serving
		return false
	}
	if segSize > 0 {
		logger.Debugf("segment %s, responded by ServeContent", seg.segId)
		cont := toContent(req.Context(), seg, segSize)
		http.ServeContent(w, req, req.URL.Path, time.Time{}, cont)
	} else {
		logger.Debugf("can't get size of segment %s, respond in stream", seg.segId)
		w.WriteHeader(http.StatusOK)
		buf := make([]byte, 8*1024)
		off := int64(0)
		flusher, canFlush := w.(http.Flusher)
		for {
			n, err := seg.ReadAt(req.Context(), buf, off)
			_, wErr := w.Write(buf[:n])
			if wErr != nil {
				logger.Errorf("write response error: %s", wErr)
				return true
			}
			if canFlush {
				flusher.Flush()
			}
			off += int64(n)
			if err == io.EOF {
				break
			} else if err != nil {
				logger.Errorf("read from segment %s error: %s", seg.segId, err)
				return true
			}
		}
	}
	return true
}

// How to Track the Client:
// 1. Basic principle: By adding a marker (such as uOptHLSUser) to the URLs of
//    playlists and segments, it is possible to know the client's current
//    playback progress.
// 2. Initially, the client requests a regular m3u8 URL (which may be a live
//    playlist or VOD playlist), which does not contain the special marker we
//    need. At this point, instead of returning the original content of that
//    m3u8, we return a multivariant playlist with the special marker inserted
//    into variant URLs. The client will then use these variant URLs to obtain
//    playlists, allowing us to track it.
// 3. Before returning playlists to clients, segment URLs are all marked with
//    special markers for easier tracking of playback progress in future.

func (h *hlsBoost) servePlaylist(w http.ResponseWriter, req *http.Request,
	opts *urlopts.Options) bool {
	playlistURI, normalizedOpts := req.URL.String(), normalizedOptions(opts)
	// hash the url with target's host and scheme
	playlistId := md5Short(toUrlproxyURI("/", playlistURI, normalizedOpts))
	opts.HLSPlaylist = &playlistId
	newUser := false
	if opts.HLSUser == nil {
		userId := genId()
		opts.HLSUser = &userId
		newUser = true
	}
	user := h.mgr.GetUser(*opts.HLSUser)

	// respond a m3u8 base on the progress
	if pl := h.mgr.GetPlaylist(playlistId); pl != nil {
		m3 := user.GetM3U8(pl)
		if newUser {
			// inject a user id to the playlist url
			m3 = getVariantM3U8(req.URL.String())
		}
		logger.Debugf("user %s get playlist %s", user.id, playlistId)
		respondRewrittenM3U8(playlistURI, m3, w, opts)
		return true
	}

	// playlist not found, sniffing the url to see if it's a m3u8
	resp, m3, err := h.sniffing(req.Context(), playlistURI, normalizedOpts)
	if err != nil {
		logger.Errorf("sniffing %s failed, err: %s, fallback to http proxy",
			req.URL, err)
		if resp != nil {
			proxy.ForwardResponse(w, resp)
			return true
		}
		return false
	}
	if isMaster(m3) {
		// master m3u8 doesn't contain any segment
		respondRewrittenM3U8(playlistURI, m3, w, opts)
		return true
	} else {
		// create a new playlist
		pl := newPlaylist(h.selfCli, playlistURI, normalizedOpts, 10*time.Second, *cacheDir)
		err := pl.Init(m3)
		if err != nil {
			logger.Errorf("initialize playlist %s failed, err: %s", pl.id, err)
			return false
		}

		// let the playlist and the user establish association before adding
		// the playlist to the manager, so that playlist will not be cleaned
		// up immediately by manager.
		m3 = user.GetM3U8(pl)
		h.mgr.AddPlaylist(playlistId, pl)

		if newUser {
			// inject a user id to the playlist url
			m3 = getVariantM3U8(req.URL.String())
		}
		respondRewrittenM3U8(playlistURI, m3, w, opts)
		return true
	}
}

func normalizedOptions(opts *urlopts.Options) *urlopts.Options {
	cloneOpts := *opts
	cloneOpts.HLSBoost = nil
	cloneOpts.HLSPlaylist = nil
	cloneOpts.HLSUser = nil
	cloneOpts.HLSSegment = nil
	cloneOpts.HLSSkip = nil
	return &cloneOpts
}

func isMaster(pl *m3u8.Playlist) bool {
	if pl.IsMaster() {
		return true
	}
	for _, it := range pl.Items {
		segItem, ok := it.(*m3u8.SegmentItem)
		if !ok {
			continue
		}
		if segItem.Duration <= 0 {
			return true
		}
	}
	return false
}

func respondRewrittenM3U8(playlistURI string, pl *m3u8.Playlist, w http.ResponseWriter, opts *urlopts.Options) {
	pl = rewriteM3U8(pl, playlistURI, opts)
	data := pl.String()
	header := w.Header()
	header.Add("Content-Type", "application/vnd.apple.mpegurl")
	header.Add("Content-Length", strconv.FormatInt(int64(len(data)), 10))
	header.Add("Cache-Control", "no-store, no-cache, must-revalidate")
	header.Add("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(data))
}

type readCloser struct {
	io.Reader
	io.Closer
}

var m3u8Header = []byte("#EXTM3U")

func (h *hlsBoost) sniffing(ctx context.Context, playlistURI string,
	reqOpts *urlopts.Options) (*http.Response, *m3u8.Playlist, error) {
	resp, err := h.selfCli.Get(ctx, "/", playlistURI, reqOpts)
	if err != nil {
		return nil, nil, err
	}
	bufrd := bufio.NewReader(resp.Body)
	resp.Body = &readCloser{
		Reader: bufrd,
		Closer: resp.Body,
	}
	if head, err := bufrd.Peek(len(m3u8Header)); err != nil {
		return resp, nil, err
	} else if !bytes.Equal(head, m3u8Header) {
		return resp, nil, fmt.Errorf("not a m3u8 file")
	}
	defer resp.Body.Close()
	pl, err := m3u8.Read(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	if err := isValid(pl); err != nil {
		logger.Warnf("get invalid m3u8 from %s", playlistURI)
		return nil, nil, err
	}
	return nil, pl, nil
}

func isValid(pl *m3u8.Playlist) error {
	if !pl.IsValid() {
		return m3u8.ErrPlaylistInvalidType
	}
	zeroDurCnt := 0
	nonZeroDurCnt := 0
	for _, it := range pl.Items {
		segItem, ok := it.(*m3u8.SegmentItem)
		if !ok {
			continue
		}
		if segItem.Duration <= 0 {
			zeroDurCnt++
		} else {
			nonZeroDurCnt++
		}
	}
	if zeroDurCnt > 0 && nonZeroDurCnt > 0 {
		return fmt.Errorf("invalid playlist, mixed playlist and segment")
	}
	return nil
}
