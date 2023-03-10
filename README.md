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

* `urlproxyOptSchema`: specify the schema for the target URL, e.g. `https`.

    ```shell
    $ curl "http://127.0.0.1:8765/httpbin.org/headers?urlproxyOptSchema=https"
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

* `urlproxyOptHeader`: add extra headers to the proxied request.

    ```shell
    $ curl "http://127.0.0.1:8765/httpbin.org/headers?urlproxyOptHeader=CustomHeader1:value1&urlproxyOptHeader=CustomHeader2:value2"
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

* `urlproxyOptSocks`: specify the upstream socks5 proxy for this request.

    ```shell
    $ curl "http://127.0.0.1:8765/httpbin.org/get?urlproxyOptSocks=127.0.0.1:1081"
    ```

    In particular, `"urlproxyOptSocks=off"` means that this request does not use the socks proxy.

* `urlproxyOptDns`: specify the DNS server used in this request.

    ```shell
    $ curl "http://127.0.0.1:8765/httpbin.org/get?urlproxyOptDns=8.8.8.8:53"
    ```

* `urlproxyOptIp`: specify the IP address of the target server for this request.

    ```shell
    $ curl "http://127.0.0.1:8765/httpbin.org/get?urlproxyOptIp=3.229.200.44"
    ```

* `urlproxyOptTimeoutMs`: specify timeout for this request, the time is including internal retries.

    ```shell
    $ curl "http://127.0.0.1:8765/httpbin.org/delay/5?urlproxyOptTimeoutMs=1000"
    ```

* `urlproxyOptRetriesNon2xx`: number of retries for non-2xx response. Note that it only supports retries for requests that use the `GET`, `HEAD`, `OPTIONS` or `TRACE` methods.

    ```shell
    $ curl "http://127.0.0.1:8765/httpbin.org/status/500?urlproxyOptRetriesNon2xx=3"
    ```

* `urlproxyOptRetriesError`: number of retries for errors (e.g. connection timeout).

    ```shell
    $ curl "http://127.0.0.1:8765/httpbin.org:1234/get?urlproxyOptRetriesError=3"
    ```

* `urlproxyOptAntiCaching`: anti-caching by adding `__t=<current time in nanoseconds>` parameter to the request URL.

    ```shell
    $ curl "http://127.0.0.1:8765/httpbin.org/get?urlproxyOptAntiCaching=true"
    ```

* `urlproxyOptRaceMode`: in race mode, urlproxy will simultaneously send several identical requests to the target server, and the first response will be used to reply to the client. The value of this parameter is a number that indicates the parallelism of the request and takes a maximum value of 5. Similar to `urlproxyOptRetriesNon2xx`, only certain http methods can use this mode.

    ```shell
    $ curl "http://127.0.0.1:8765/httpbin.org/get?urlproxyOptRaceMode=2"
    ```

### Alternate Url Pattern

Control parameters can be placed in path in addition to the url parameter. This can be useful in some cases.

```shell
$ curl "http://127.0.0.1:8765/urlproxyOptSchema=https/urlproxyOptHeader=User-Agent:MyClient/httpbin.org/headers"
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

# License

MIT
