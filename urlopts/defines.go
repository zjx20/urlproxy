package urlopts

import (
	"fmt"
	"net/url"
)

var (
	options = map[string]Option{}
)

var (
	OptHost            = defineStringOption("Host")
	OptHeader          = defineHeaderOption("Header")
	OptScheme          = defineStringOption("Scheme")
	OptSocks           = defineStringOption("Socks")
	OptDns             = defineStringOption("Dns")
	OptIp              = defineStringOption("Ip")
	OptTimeoutMs       = defineInt64Option("TimeoutMs")
	OptRetriesNon2xx   = defineInt64Option("RetriesNon2xx")
	OptRetriesError    = defineInt64Option("RetriesError")
	OptAntiCaching     = defineBoolOption("AntiCaching")
	OptRaceMode        = defineInt64Option("RaceMode")
	OptRewriteRedirect = defineBoolOption("RewriteRedirect")

	OptHLSBoost      = defineBoolOption("HLSBoost")
	OptHLSPrefetches = defineInt64Option("HLSPrefetches")
	OptHLSPlaylist   = defineStringOption("HLSPlaylist") // internal
	OptHLSUser       = defineStringOption("HLSUser")     // internal
	OptHLSSegment    = defineStringOption("HLSSegment")  // internal
	OptHLSSkip       = defineBoolOption("HLSSkip")       // internal

	OptAntPieceSize        = defineInt64Option("AntPieceSize")
	OptAntConcurrentPieces = defineInt64Option("AntConcurrentPieces")
)

//////////////////////////////////////////////////////////////////////////////

type identifier[O Option, V any] struct {
	name string
}

func (id identifier[O, V]) Name() string {
	return id.name
}

func (id *identifier[O, V]) New(v V) Option {
	o := options[id.name]
	if o == nil {
		panic(fmt.Sprintf("unknown option %s", id.name))
	}
	o = o.Clone()
	o.ObscureSet(v)
	return o
}

func (id *identifier[O, V]) ValueFrom(opts *Options) (ret V, ok bool) {
	v, ok := opts.optMap.Load(id.name)
	if !ok {
		return ret, false
	}
	o := v.(O)
	if !o.IsPresent() {
		return ret, false
	}
	ret, ok = o.ObscureValue().(V)
	return
}

func (id *identifier[O, V]) ExistsIn(opts *Options) bool {
	_, exists := id.ValueFrom(opts)
	return exists
}

func addDefinition[O Option, V any](name string, opt O, _ V) identifier[O, V] {
	id := identifier[O, V]{
		name: name,
	}
	options[id.name] = opt
	return id
}

func defineInt64Option(name string) identifier[*Int64Option, int64] {
	o := &Int64Option{}
	o.name = name
	return addDefinition(name, o, o.Value())
}

func defineStringOption(name string) identifier[*StringOption, string] {
	o := &StringOption{}
	o.name = name
	return addDefinition(name, o, o.Value())
}

func defineHeaderOption(name string) identifier[*HeaderOption, url.Values] {
	o := &HeaderOption{}
	o.name = name
	return addDefinition(name, o, o.Value())
}

func defineBoolOption(name string) identifier[*BoolOption, bool] {
	o := &BoolOption{}
	o.name = name
	return addDefinition(name, o, o.Value())
}
