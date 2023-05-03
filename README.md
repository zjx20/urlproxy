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

* `uOptHLSPrefetches`: Specifies the number of concurrent downloads for segments; default value is 1.

* `uOptAntConcurrentPieces`: Specifies the number of threads used for multi-threaded downloads; setting this value to 1 disables multi-threaded downloads; default value is 5.

* `uOptAntPieceSize`: If number of download threads are greater than 1, specifies how many bytes per thread will be downloaded at once; default value is 524288 (512KB).

# License

MIT
