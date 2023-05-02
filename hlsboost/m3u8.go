package hlsboost

import (
	"github.com/zjx20/urlproxy/urlopts"

	"github.com/etherlabsio/go-m3u8/m3u8"
)

func rewriteM3U8(pl *m3u8.Playlist, parentURI string, opts *urlopts.Options) *m3u8.Playlist {
	clonePl := *pl
	clonePl.Items = append([]m3u8.Item{}, pl.Items...)
	for idx, x := range clonePl.Items {
		switch it := x.(type) {
		case *m3u8.PlaylistItem:
			clone := *it
			cloneOpts := *opts
			cloneOpts.HLSBoost = &trueVal
			cloneOpts.HLSPlaylist = nil
			cloneOpts.HLSSegment = nil
			clone.URI = toUrlproxyURI(parentURI, clone.URI, &cloneOpts)
			clonePl.Items[idx] = &clone
		case *m3u8.SessionKeyItem:
			if it.Encryptable != nil && it.Encryptable.URI != nil {
				clone := *it
				uri := toUrlproxyURI(parentURI, *clone.Encryptable.URI, opts)
				clone.Encryptable.URI = &uri
				clonePl.Items[idx] = &clone
			}
		case *m3u8.SessionDataItem:
			if it.URI != nil {
				clone := *it
				uri := toUrlproxyURI(parentURI, *clone.URI, opts)
				clone.URI = &uri
				clonePl.Items[idx] = &clone
			}
		case *m3u8.MediaItem:
			if it.URI != nil {
				clone := *it
				uri := toUrlproxyURI(parentURI, *clone.URI, opts)
				clone.URI = &uri
				clonePl.Items[idx] = &clone
			}
		case *m3u8.MapItem:
			clone := *it
			clone.URI = toUrlproxyURI(parentURI, clone.URI, opts)
			clonePl.Items[idx] = &clone
		case *m3u8.KeyItem:
			if it.Encryptable != nil && it.Encryptable.URI != nil {
				clone := *it
				uri := toUrlproxyURI(parentURI, *clone.Encryptable.URI, opts)
				clone.Encryptable.URI = &uri
				clonePl.Items[idx] = &clone
			}
		case *m3u8.SegmentItem:
			clone := *it
			cloneOpts := *opts
			if it.Duration > 0 {
				segId := md5Short(clone.Segment)
				cloneOpts.HLSBoost = nil
				cloneOpts.HLSSegment = &segId
			} else {
				// it's a playlist item if duration <= 0
				cloneOpts.HLSBoost = &trueVal
				cloneOpts.HLSPlaylist = nil
				cloneOpts.HLSSegment = nil
			}
			clone.Segment = toUrlproxyURI(parentURI, clone.Segment, &cloneOpts)
			clonePl.Items[idx] = &clone
		}
	}
	return &clonePl
}

func getVariantM3U8(uri string) *m3u8.Playlist {
	pl := m3u8.NewPlaylist()
	plItem := &m3u8.PlaylistItem{
		Bandwidth: 2000000,
		URI:       uri,
		IFrame:    false,
	}
	pl.AppendItem(plItem)
	return pl
}
