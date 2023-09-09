package urlopts

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const UrlOptionPrefix = "uOpt"

type Option interface {
	Name() string
	Clear()
	IsPresent() bool
	ObscureValue() any
	ObscureSet(v any)
	Parse(s string) error
	ToUrlOption() []string
	Clone() Option
}

type OptionBase[T any] struct {
	name    string
	present bool
	value   T
}

func (ob *OptionBase[T]) Name() string {
	return ob.name
}

func (ob *OptionBase[T]) Clear() {
	var zero T
	ob.value = zero
	ob.present = false
}

func (ob *OptionBase[T]) IsPresent() bool {
	return ob.present
}

func (ob *OptionBase[T]) ObscureValue() any {
	return ob.value
}

func (ob *OptionBase[T]) Value() T {
	return ob.value
}

func (ob *OptionBase[T]) ObscureSet(v any) {
	ob.Set(v.(T))
}

func (ob *OptionBase[T]) Set(v T) {
	ob.value = v
	ob.present = true
}

func (ob *OptionBase[T]) urlOptionKey() string {
	return UrlOptionPrefix + ob.name + "="
}

//////////////////////////////////////////////////////////////////////////////

type Int64Option struct {
	OptionBase[int64]
}

func (o *Int64Option) Parse(s string) error {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return err
	}
	o.Set(v)
	return nil
}

func (o *Int64Option) ToUrlOption() []string {
	return []string{o.urlOptionKey() + strconv.FormatInt(o.Value(), 10)}
}

func (o *Int64Option) Clone() Option {
	co := *o
	return &co
}

//////////////////////////////////////////////////////////////////////////////

type StringOption struct {
	OptionBase[string]
}

func (o *StringOption) Parse(s string) error {
	o.Set(s)
	return nil
}

func (o *StringOption) ToUrlOption() []string {
	return []string{o.urlOptionKey() + url.PathEscape(o.Value())}
}

func (o *StringOption) Clone() Option {
	co := *o
	return &co
}

//////////////////////////////////////////////////////////////////////////////

type HeaderOption struct {
	OptionBase[http.Header]
}

func (o *HeaderOption) Parse(s string) error {
	v := o.Value()
	if v == nil {
		v = http.Header{}
	}
	pos := strings.Index(s, ":")
	if pos == -1 {
		// key only header
		v.Add(s, "")
	} else {
		parts := strings.SplitN(s, ":", 2)
		val, err := url.PathUnescape(parts[1])
		if err != nil {
			val = parts[1]
		}
		v.Add(parts[0], strings.TrimSpace(val))
	}
	o.Set(v)
	return nil
}

func (o *HeaderOption) ToUrlOption() []string {
	var result []string
	headers := o.Value()
	for key, values := range headers {
		for _, value := range values {
			result = append(result, o.urlOptionKey()+key+":"+url.PathEscape(value))
		}
	}
	return result
}

func (o *HeaderOption) Clone() Option {
	co := *o
	headers := o.Value()
	if len(headers) == 0 {
		return &co
	}
	newHeaders := http.Header{}
	for key, values := range headers {
		newHeaders[key] = append([]string{}, values...)
	}
	co.Set(newHeaders)
	return &co
}

//////////////////////////////////////////////////////////////////////////////

var (
	boolVals = map[string]bool{
		"true":  true,
		"yes":   true,
		"on":    true,
		"1":     true,
		"false": false,
		"no":    false,
		"off":   false,
		"0":     false,
	}
)

type BoolOption struct {
	OptionBase[bool]
}

func (o *BoolOption) Parse(s string) error {
	v, ok := boolVals[strings.ToLower(s)]
	if !ok {
		return fmt.Errorf("invalid bool value: %s", s)
	}
	o.Set(v)
	return nil
}

func (o *BoolOption) ToUrlOption() []string {
	return []string{o.urlOptionKey() + strconv.FormatBool(o.Value())}
}

func (o *BoolOption) Clone() Option {
	co := *o
	return &co
}
