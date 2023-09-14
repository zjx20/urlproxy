urlproxy
========

**urlproxy** is an HTTP reverse proxy that allows for dynamic routing without the need for pre-defined rules like traditional reverse proxies such as Caddy. It accomplishes this by encoding the target website's address into the request URL. In addition, it can also chain to an upstream SOCKS5 proxy.

# Install

Install to `$GOPATH/bin`:

```bash
go install github.com/zjx20/urlproxy@latest
```

Compiling from source code:

```bash
go build -o urlproxy main.go
```

# Run

```shell
$ ./urlproxy -h
Usage of ./urlproxy:
  -bind string
    	Address to bind (default "0.0.0.0:8765")
  -debug
    	Verbose logs
  -file-root string
    	Root path for the file scheme
  -socks string
    	Upstream socks5 proxy, e.g. 127.0.0.1:1080
  -socks-uds string
    	Path of unix domain socket for upstream socks5 proxy
```

Simply run `./urlproxy`, and urlproxy will listen on 8765 by default.

# Usage

Assume that the target URL is:

```
http://website.com/some/path?param1=val1&param2=val2
```

Then the request URL to urlproxy should be:

```
http://127.0.0.1:8765/website.com/some/path?param1=val1&param2=val2
```

That's it. Headers are forwarded to the target server as well, and urlproxy follows redirects automatically.

## Options

There are some special url parameters that can further control the proxy behavior.

* `uOptScheme`: specify the scheme for the target URL, e.g. `https`.

    ```shell
    $ curl "http://127.0.0.1:8765/httpbin.org/headers?uOptScheme=https"
    {
        "headers": {
            "Accept": "*/*",
            "Accept-Encoding": "gzip",
            "Host": "httpbin.org",
            "User-Agent": "curl/7.79.1",
            "X-Amzn-Trace-Id": "Root=1-63b853d8-2ec483150e898d8470823f62"
        }
    }
    ```

    `file` scheme is also supported. But only files inside the `-file-root` folder are allowed to read. This turns `urlproxy` into an HTTP server for hosting static files. For example:

    ```shell
    $ echo hello > /tmp/hello.txt
    $ ./urlproxy -file-root /tmp &
    $ curl "http://localhost:8765/hello.txt?uOptScheme=file"
    hello
    ```

    Another special scheme is `tpl`, see the [Template Rendering](#template-rendering) section for more details.

* `uOptHeader`: add extra headers to the proxied request.

    ```shell
    $ curl "http://127.0.0.1:8765/httpbin.org/headers?uOptHeader=CustomHeader1:value1&uOptHeader=CustomHeader2:value2"
    {
        "headers": {
            "Accept": "*/*",
            "Accept-Encoding": "gzip",
            "Customheader1": "value1",
            "Customheader2": "value2",
            "Host": "httpbin.org",
            "User-Agent": "curl/7.79.1",
            "X-Amzn-Trace-Id": "Root=1-63b8548b-78020fa9171479c72b1a422c"
        }
    }
    ```

* `uOptRespHeader`: add extra headers to the response.

    ```shell
    $ curl -v "http://127.0.0.1:8765/httpbin.org/headers?uOptRespHeader=CustomHeader1:value1&uOptRespHeader=CustomHeader2:value2" > /dev/null
    Trying 127.0.0.1:8765...
    * Connected to 127.0.0.1 (127.0.0.1) port 8765 (#0)
    > GET /httpbin.org/headers?uOptRespHeader=CustomHeader1:value1&uOptRespHeader=CustomHeader2:value2 HTTP/1.1
    > Host: 127.0.0.1:8765
    > User-Agent: curl/7.79.1
    > Accept: */*
    >
    * Mark bundle as not supporting multiuse
    < HTTP/1.1 200 OK
    < Access-Control-Allow-Credentials: true
    < Access-Control-Allow-Origin: *
    < Connection: keep-alive
    < Content-Length: 297
    < Content-Type: application/json
    < Customheader1: value1
    < Customheader2: value2
    < Date: Fri, 08 Sep 2023 13:38:13 GMT
    < Server: gunicorn/19.9.0
    <
    * Connection #0 to host 127.0.0.1 left intact
    ```

* `uOptSocks`: specify the upstream socks5 proxy for this request.

    ```shell
    $ curl "http://127.0.0.1:8765/httpbin.org/get?uOptSocks=127.0.0.1:1081"
    ```

    In particular, `"uOptSocks=off"` means that this request does not use the socks proxy.

* `uOptDns`: specify the DNS server used in this request.

    ```shell
    $ curl "http://127.0.0.1:8765/httpbin.org/get?uOptDns=8.8.8.8:53"
    ```

* `uOptIp`: specify the IP address of the target server for this request.

    ```shell
    $ curl "http://127.0.0.1:8765/httpbin.org/get?uOptIp=3.229.200.44"
    ```

* `uOptTimeoutMs`: specify timeout for this request, the time is including internal retries.

    ```shell
    $ curl "http://127.0.0.1:8765/httpbin.org/delay/5?uOptTimeoutMs=1000"
    ```

* `uOptRetriesNon2xx`: number of retries for non-2xx response. Note that it only supports retries for requests that use the `GET`, `HEAD`, `OPTIONS` or `TRACE` methods.

    ```shell
    $ curl "http://127.0.0.1:8765/httpbin.org/status/500?uOptRetriesNon2xx=3"
    ```

* `uOptRetriesError`: number of retries for errors (e.g. connection timeout).

    ```shell
    $ curl "http://127.0.0.1:8765/httpbin.org:1234/get?uOptRetriesError=3"
    ```

* `uOptAntiCaching`: anti-caching by adding `__t=<current time in nanoseconds>` parameter to the request URL.

    ```shell
    $ curl "http://127.0.0.1:8765/httpbin.org/get?uOptAntiCaching=true"
    ```

* `uOptRaceMode`: in race mode, urlproxy will simultaneously send several identical requests to the target server, and the first response will be used to reply to the client. The value of this parameter is a number that indicates the parallelism of the request and takes a maximum value of 5. Similar to `uOptRetriesNon2xx`, only certain http methods can use this mode.

    ```shell
    $ curl "http://127.0.0.1:8765/httpbin.org/get?uOptRaceMode=2"
    ```

* `uOptRewriteRedirect`: `urlproxy` does not automatically follow redirects (such as 301 and 302 status codes), but returns the http status code and `Location` to the client. When redirecting, the client may not send request to `urlproxy`, but directly connect to the target address. This option can rewrite the redirect, changing the `Location` to an address pointing to `urlproxy`.

    ```shell
    $ curl "http://127.0.0.1:8765/httpbin.org/redirect-to?url=http://google.com&status_code=302&uOptRewriteRedirect=true"
    ```

* `uOptPipe`: the content of this parameter is a shell script. When the proxy request is successful (such as the response code is 200), `urlproxy` will execute this script through `/bin/sh`, and use the body of the proxy response as the stdin of `exec.Cmd`, and then forward the stdout of `exec.Cmd` to the http client.

    ```shell
    $ curl "http://127.0.0.1:8765/httpbin.org/get?uOptPipe=sed%20%27s%2Fhttpbin.org%2Fgro.nibptth%2Fg%27"

    # The command is equivalent to:
    #   curl -s "http://127.0.0.1:8765/httpbin.org/get" | sed 's/httpbin.org/gro.nibptth/g'
    ```

    **Note**: This feature is powerful but also very dangerous. Therefore, the `uOptPipe` option will only take effect when the `-enable-uoptpipe` command-line parameter was added to start `urlproxy`. Please make sure not to deploy this feature to the public network.

* `uOptQueryParams`: add extra query parameters to the proxied request. It's useful for passing `uOpt*` to the proxied request.

    ```shell
    $ curl "http://127.0.0.1:8765/httpbin.org/get?foo=bar&uOptQueryParams=hello%3Dworld"
    ```

### Alternate Url Pattern

Options can be placed in the path with the format of `/uOptXXX=XXX/`. This can be useful in some cases.

```shell
$ curl "http://127.0.0.1:8765/uOptScheme=https/uOptHeader=User-Agent:MyClient/httpbin.org/headers"
{
  "headers": {
    "Accept": "*/*",
    "Accept-Encoding": "gzip",
    "Host": "httpbin.org",
    "User-Agent": "MyClient",
    "X-Amzn-Trace-Id": "Root=1-63b99e95-439b4d3a6725f13a04464629"
  }
}
```

## Forward Proxy

**urlproxy** can also act as a regular HTTP proxy, this allows it to function as an HTTP-to-SOCKS proxy.

```shell
export http_proxy=http://127.0.0.1:8765
curl -v http://httpbin.org/get
```

## Template Rendering

`urlproxy` treats [Go Template](https://pkg.go.dev/text/template) as a programming language for handling http requests (similar to PHP), which allows for some complex data processing. This is equivalent to implementing `func ServeHTTP(w http.ResponseWriter, r *http.Request)` with Go Template, so the `http.Request` and `http.ResponseWriter` objects of the current request are available in the template context. Request data such as query parameters can be retrieved by using the `http.Request` object. For the response, the status code, headers and body can be set using the `http.ResponseWriter` object. The render result of the template will also be appended to the response body.

When starting `urlproxy`, there is a `-tpl-root` parameter that specifies the directory where the template files are located so that they can be referenced in subsequent requests. If the request path points to the template file (relative to `-tpl-root`) and the `uOptScheme=tpl` parameter is given, `urlproxy` will handle the http request using the specified template.

The following is an example template. It first retrieves the value of the `"value"` parameter from the request, constructs an http request, retrieves data from httpbin.org, then parses the returned data into a map, iterates over key-value pairs for output, and finally sets the status code to 500 and appends a header named `"Marker"`.

```go-template
{{- /* make a request */ -}}
{{- $myheader := httpHeader "MyHeader" (.Request.URL.Query.Get "value") -}}
{{- $resp := httpReq nil "GET" "https://httpbin.org/headers" "" $myheader 10 -}}

{{- /* parse the response from httpbin.org, and then render it, as the response body */ -}}
{{- $respObj := $resp.Text | fromJson -}}
{{ range $key, $value := $respObj.headers }}
  {{- printf "%s: %v" $key $value }}
{{ end }}

{{- /* It can also use .ResponseWriter.WriteString to write data to the response body.
       However, it's not recommended to mix the usage of WriteString and the text
       rendering, because their output order are not determined. */ -}}
{{- .ResponseWriter.WriteString "Output with .ResponseWriter.WriteString.\n" }}
{{ range $key, $value := $respObj.headers }}
  {{- .ResponseWriter.WriteString (printf "%s: %v\n" $key $value) }}
{{ end }}

{{- /* control the response header */ -}}
{{- $_ := .ResponseWriter.Header.Set "Marker" "handled by Go Template" -}}
{{- $_ := .ResponseWriter.WriteHeader 500 -}}
```

Save the above template to `./tplfiles/addheader.tpl` and run the following command.

```shell
# start urlproxy
$ ./urlproxy -tpl-root ./tplfiles &

# invoke addheader.tpl
$ curl -v "http://127.0.0.1:8765/addheader.tpl?value=hello&uOptScheme=tpl"
*   Trying 127.0.0.1:8765...
* Connected to 127.0.0.1 (127.0.0.1) port 8765 (#0)
> GET /addheader.tpl?uOptScheme=tpl&value=hello HTTP/1.1
> Host: 127.0.0.1:8765
> User-Agent: curl/7.79.1
> Accept: */*
>
* Mark bundle as not supporting multiuse
< HTTP/1.1 500 Internal Server Error
< Marker: handled by Go Template
< Date: Sun, 10 Sep 2023 10:34:48 GMT
< Content-Length: 145
< Content-Type: text/plain; charset=utf-8
<
Accept-Encoding: gzip
Host: httpbin.org
Myheader: hello
User-Agent: Go-http-client/2.0
X-Amzn-Trace-Id: Root=1-64fd9bc7-2c13ea207781cdee55c3865e
* Connection #0 to host 127.0.0.1 left intact
```

Besides referencing a `.tpl` file, the template can also be inlined in the `"inline"` parameter. For example:

```shell
# the inlined template is `sha1sum of "hello" is {{ sha1sum "hello" }}`
$ curl "http://127.0.0.1:8765/?uOptScheme=tpl&inline=sha1sum%20of%20%22hello%22%20is%20%7B%7B%20sha1sum%20%22hello%22%20%7D%7D"
# the output is
sha1sum of "hello" is aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d
```

### Template Functions

All functions from [slim-sprig](https://github.com/go-task/slim-sprig) are available in the template context. `urlproxy` also provides other useful functions.

* `httpReq(ctx, method, url, body, headers, timeoutSec)` - Initiates an http request. Returns a `ResponseWrapper` object.

    ```go-template
    {{- $resp := httpReq nil "GET" "https://httpbin.org/get" "" nil 10 -}}

    {{- $resp := httpReq nil "POST" "https://httpbin.org/post" "hello" nil 10 -}}

    {{- $resp := httpReq nil "GET" "https://httpbin.org/headers" "" (httpHeader "HeaderName" "value") 10 -}}

    {{- /* uses the ctx from .Request, httpReq will be interrupted if the client disconnects. */ -}}
    {{- $resp :=httpReq .Request.Context "GET" "https://httpbin.org/delay/10" "" nil 60 -}}
    ```

    The `ResponseWrapper` object embeds a `http.Response`, so all the fields in the `http.Response` can be accessed by the `ResponseWrapper.Response.XXX`. `ResponseWrapper` also comes with additional methods to handle the http response.

    ```go-template
    {{- /* Text(): it returns the whole response body as a string */ -}}
    {{- $resp := httpReq nil "GET" "https://httpbin.org/get" "" nil 10 -}}
    StatusCode: {{ $resp.Response.StatusCode }}
    Response: {{ $resp.Text }}

    {{- /* LastLocation(): httpReq automatically follows redirects,
                           so LastLocation() can be used to get the last location */ -}}
    {{- $url := "https://httpbin.org/redirect-to?url=https://httpbin.org/base64/aGVsbG8K" -}}
    {{- $resp := httpReq nil "GET" $url "" nil 10 -}}
    Url: {{ $url }}
    Last Location: {{ $resp.LastLocation }}
    ```

* `tryHttpReq(ctx, method, url, body, headers, timeoutSec)` - It is the same as `httpReq` except that it does not abort the execution of the template in case of an error. If it does, `ResponseWrapper.Response` will be `nil` and the error can be retrieved using `ResponseWrapper.Error`.

    ```go-template
    {{- /* will hit a "deadline exceeded" error */ -}}
    {{- $resp := tryHttpReq nil "GET" "https://httpbin.org/delay/10" "" nil 1 -}}
    {{- if $resp.Error -}}
      {{ printf "request failed, err: %s" $resp.Error }}
    {{- end -}}
    ```

* `httpHeader(v ...string)` - Creates a `HeaderWrapper` (mainly used for `httpReq`) with an even number of arguments.

    ```go-template
    {{- httpHeader "Header1" "value1" "Header2" "value2" -}}
    ```

* `parseUrl(url)` - Parses the url and returns a `url.URL` object.

    ```go-template
    {{- (parseUrl "https://httpbin.org/").Host -}}
    ```

* `urlproxiedUrl(url, urlproxyBaseUrl, uopts ...string)` - Converts to a proxied url (then used with `httpReq`).

    ```go-template
    {{- urlproxiedUrl "https://httpbin.org/" "http://127.0.0.1:8765/" "uOptScheme" "http" "uOptTimeoutMs" "3000" -}}
    ```

    The address of the current `urlproxy` instance can be obtained via the template variable `.ExtraValues.urlproxyBaseUrl`.

    ```go-template
    {{- urlproxiedUrl "https://httpbin.org/" .ExtraValues.urlproxyBaseUrl -}}
    ```

* `save(namespace, key, value)` - Stores a key value in the persistent store. The location of the store can be specified with the `-kvstore-dir` flag when starting `urlproxy`. Note that `save` may fail for various reasons, the return value indicates success or not.

    ```go-template
    {{- $ok := save "example" "foo" "bar" -}}
    ```

* `mustSave(namespace, key, value)` - The same as `save` except that it aborts the execution if an error occurs.

    ```go-template
    {{- mustSave "example" "foo" "bar" -}}
    ```

* `load(namespace, key)` - Loads the previously saved value. Returns an empty string if the key is not found or an error has occurred.

    ```go-template
    {{- load "example" "foo" -}}
    ```

* `mustLoad(namespace, key)` - The same as `load` except that it aborts the execution if an error occurs.

    ```go-template
    {{- mustLoad "example" "foo" -}}
    ```

* `execTemplate(name, data)` - It executes the sub-template defined by the `define` action and captures the render result. The idea comes from this [SO answer](https://stackoverflow.com/a/40170999).

    ```go-template
    {{- define "greet" -}}
      {{- printf "Hello %s!" . -}}
    {{- end -}}

    {{- /* $msg should be "Hello Bob!" */ -}}
    {{- $msg := execTemplate "greet" "Bob" -}}
    ```

* `debug(format, ...any)` - It writes a message to the urlproxy log with DEBUG level. The `-debug` parameter should be set when starting `urlproxy` to see these logs.

    ```go-template
    {{- $_ := debug "Hello %s!" "Bob" -}}
    ```

## HLSBoost

`urlproxy` can recognize HLS (HTTP Live Streaming) content and accelerate it, the technology is called `HLSBoost`. Typically, HLS players download segments in a conservative manner, such as downloading segments one by one in sequence with a single thread. In poor network conditions, this can easily lead to playback stuttering. `HLSBoost` works between the HLS player and server, intercepts and caches segment files by sniffing and hijacking the m3u8 playlist. It employs an aggressive prefetch strategy that tracks the client's playback progress to cache required content ahead of time, thereby reducing playback stuttering. Additionally, when servers have TCP connection-level speed limits, multi-threaded slicing download technology used by `HLSBoost` can effectively increase download speeds.

### Usage

Suppose there is an HLS playback link:

```
http://example.com/hls/index.m3u8
```

Convert it to `urlproxy` style link with `uOptHLSBoost=true` parameter included:

```
http://127.0.0.1:8765/uOptHLSBoost=true/example.com/hls/index.m3u8
```

Then give the proxied link to the player for accelerated performance!

### Optional Parameters

* `uOptHLSPrefetches`: Specifies the number of concurrent downloads for segments; default value is 3 and set to 0 to disable prefetching.

* `uOptAntConcurrentPieces`: Specifies the number of threads used for multi-threaded downloads; setting this value to 1 disables multi-threaded downloads; default value is 5.

* `uOptAntPieceSize`: If number of download threads are greater than 1, specifies how many bytes per thread will be downloaded at once; default value is 524288 (512KB).

* `uOptHLSTimeoutMs`: Timeout for fetching `m3u8` playlist or segments; default value is 5000 (5 seconds).

# License

MIT
