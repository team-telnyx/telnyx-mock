# telnyx-mock [![Build Status](https://travis-ci.org/team-telnyx/telnyx-mock.svg?branch=master)](https://travis-ci.org/team-telnyx/telnyx-mock)

telnyx-mock is a mock HTTP server that responds like the real Telnyx API. It
can be used instead of Telnyx's test mode to make test suites integrating with
Telnyx faster and less brittle. It's powered by [the Telnyx OpenAPI
specification][openapi], which is generated from within Telnyx's API.

## Current state of development

telnyx-mock is able to generate an approximately correct API response for any
endpoint, but the logic for doing so is still quite naive. It supports the
following features:

* It has a catalog of every API URL and their signatures. It responds on URLs
  that exist with a resource that it returns and 404s on URLs that don't exist.
* JSON Schema is used to check the validity of the parameters of incoming
  requests. Validation is comprehensive, but far from exhaustive, so don't
  expect the full barrage of checks of the live API.
* Responses are generated based off resource fixtures. They're also generated
  from within Telnyx's API, and similar to the sample data available in
  Telnyx's [API reference][apiref].
* It reflects the values of valid input parameters into responses where the
  naming and type are the same. So if a messaging profile is created with `name=foo`, a
  messaging profile will be returned with `"name": "foo"`.
* It will respond over HTTP or over HTTPS. HTTP/2 over HTTPS is available if
  the client supports it.

Limitations:

* It's currently stateless. Data created with `POST` of `PATCH` calls won't be stored so
  that the same information is available later.
* For polymorphic endpoints, only a single resource type is ever returned. There's no way to
  specify which one that is.
* It's locked to the latest version of Telnyx's API and doesn't support old
  versions.
* Testing for specific responses and error is currently not supported.
  It will return a success response instead of the desired error response.

## Usage

If you have Go installed, you can install the basic binary with:

``` sh
go get -u github.com/team-telnyx/telnyx-mock
```

With no arguments, telnyx-mock will listen with HTTP on its default port of
`12111` and HTTPS on `12112`:

``` sh
telnyx-mock
```

Ports can be specified explicitly with:

``` sh
telnyx-mock -http-port 12111 -https-port 12112
```

(Leave either `-http-port` or `-https-port` out to activate telnyx-mock on only
one protocol.)

Have telnyx-mock select a port automatically by passing `0`:

``` sh
telnyx-mock -http-port 0
```

It can also listen via Unix socket:

``` sh
telnyx-mock -http-unix /tmp/telnyx-mock.sock -https-unix /tmp/telnyx-mock-secure.sock
```

### Homebrew

Get it from Homebrew or download it [from the releases page][releases]:

``` sh
brew install telnyx/telnyx-mock/telnyx-mock

# start a telnyx-mock service at login
brew services start telnyx-mock

# upgrade if you already have it
brew upgrade telnyx-mock
```

The Homebrew service listens on port `12111` for HTTP and `12112` for HTTPS and
HTTP/2.

### Docker

``` sh
# build
docker build . -t telnyx-mock
# run
docker run -p 12111-12112:12111-12112 telnyx-mock
```

The default Docker `ENTRYPOINT` listens on port `12111` for HTTP and `12112`
for HTTPS and HTTP/2.

### Sample request

After you've started telnyx-mock, you can try a sample request against it:

``` sh
curl -i http://localhost:12111/v2/messaging_profiles -H "Authorization: Bearer
KEYSUPERSECRET"
```

## Development

### Testing

Run the test suite:

``` sh
go test ./...
```

### Binary data & updating OpenAPI

The project uses [go-bindata] to bundle OpenAPI and fixture data into
`bindata.go` so that it's automatically included with built executables.

You can retrieve the latest OpenAPI spec from
https://api.telnyx.com/v2/mission_control_docs

Pretty format the json and overwrite the `spec3.json` file in
`openapi/openapi/`

Rebuild it with:

``` sh
# Make sure you have the go-bindata executable (it's not vendored into this
# repository).
go get -u github.com/jteeuwen/go-bindata/...

# Generates `bindata.go`.
go generate
```

## Release

Release builds are generated with [goreleaser]. Make sure you have the software
and a `GITHUB_TOKEN`:

``` sh
go get -u github.com/goreleaser/goreleaser
export GITHUB_TOKEN=...
```

Commit changes and tag `HEAD`:

``` sh
git pull origin --tags
git tag v0.1.1
git push origin --tags
```

Then run goreleaser and you're done! Check [releases] (it also pushes to the
Homebrew tap).

``` sh
goreleaser --rm-dist
```

[apiref]: https://developers.telnyx.com
[go-bindata]: https://github.com/jteeuwen/go-bindata
[goreleaser]: https://github.com/goreleaser/goreleaser
[openapi]: https://api.telnyx.com/v2/mission_control_docs
[releases]: https://github.com/team-telnyx/telnyx-mock/releases

## Acknowledgements

The contributors and maintainers of Telnyx Mock would like to extend their deep gratitude to the
authors of [Stripe Mock](https://github.com/stripe/stripe-mock), upon which
this project is based. Thank you for developing such elegant, usable, and extensible code
and for sharing it with the community.

<!--
# vim: set tw=79:
-->
