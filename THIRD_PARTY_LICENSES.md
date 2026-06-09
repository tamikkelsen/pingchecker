# Third-Party Licenses

PingChecker is distributed under the [MIT License](LICENSE). The compiled Go
binary statically links the dependencies listed below. All are permissive
(MIT / BSD-2-Clause / BSD-3-Clause) and compatible with MIT redistribution.
Their copyright notices and license texts are reproduced here as required.

## Go dependencies (bundled into the binary)

| Module | Version | License | Copyright |
|--------|---------|---------|-----------|
| github.com/gorilla/websocket | v1.5.3 | BSD-2-Clause | © 2013 The Gorilla WebSocket Authors |
| modernc.org/sqlite | v1.52.0 | BSD-3-Clause | © 2017 The Sqlite Authors |
| github.com/dustin/go-humanize | v1.0.1 | MIT | © 2005-2008 Dustin Sallings |
| github.com/google/uuid | v1.6.0 | BSD-3-Clause | © 2009, 2014 Google Inc. |
| github.com/mattn/go-isatty | v0.0.20 | MIT | © Yasuhiro Matsumoto |
| github.com/ncruces/go-strftime | v1.0.0 | MIT | © 2022 Nuno Cruces |
| github.com/remyoudompheng/bigfft | (pseudo-version) | BSD-3-Clause | © 2012 The Go Authors |
| golang.org/x/sys | v0.42.0 | BSD-3-Clause | © 2009 The Go Authors |
| modernc.org/libc | v1.72.3 | BSD-3-Clause | © 2017 The Libc Authors |
| modernc.org/mathutil | v1.7.1 | BSD-3-Clause | © 2014 The mathutil Authors |
| modernc.org/memory | v1.11.0 | BSD-3-Clause | © 2017 The Memory Authors |

## Legacy Python dependencies (NOT bundled; installed via pip when running `legacy/`)

These are only used by the optional legacy Python implementation in `legacy/`
and are installed separately by the end user via `pip`; they are not
redistributed by this repository.

| Package | License |
|---------|---------|
| fastapi | MIT |
| uvicorn | BSD-3-Clause |
| aiosqlite | MIT |

---

## License texts

### MIT License (Expat)

Applies to: github.com/dustin/go-humanize, github.com/mattn/go-isatty,
github.com/ncruces/go-strftime (copyrights as listed above).

```
Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

### BSD-2-Clause License

Applies to: github.com/gorilla/websocket (© 2013 The Gorilla WebSocket Authors).

```
Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

 1. Redistributions of source code must retain the above copyright notice,
    this list of conditions and the following disclaimer.

 2. Redistributions in binary form must reproduce the above copyright notice,
    this list of conditions and the following disclaimer in the documentation
    and/or other materials provided with the distribution.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

### BSD-3-Clause License

Applies to: modernc.org/sqlite, modernc.org/libc, modernc.org/mathutil,
modernc.org/memory, github.com/google/uuid, github.com/remyoudompheng/bigfft,
golang.org/x/sys (copyrights as listed above).

```
Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

 1. Redistributions of source code must retain the above copyright notice,
    this list of conditions and the following disclaimer.

 2. Redistributions in binary form must reproduce the above copyright notice,
    this list of conditions and the following disclaimer in the documentation
    and/or other materials provided with the distribution.

 3. Neither the name of the copyright holder nor the names of its contributors
    may be used to endorse or promote products derived from this software
    without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```
