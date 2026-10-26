# Vendored third-party scripts

## plotly-2.35.2.min.js

- Version: Plotly.js 2.35.2 (the version `cfd/template.html` loads from the CDN)
- Sources (byte-identical when downloaded):
  - https://cdn.plot.ly/plotly-2.35.2.min.js
  - https://cdn.jsdelivr.net/npm/plotly.js-dist-min@2.35.2/plotly.min.js
- SHA-256: `6d21266ce1bd7d9e5ab4e115989c70c20de0382fd973a8f26ab58619eba4d603`
- Licence: MIT

Never edit this file. To upgrade Plotly, replace the file and bump the
`plotlyVersion`/`plotlySHA256` constants in `static.go` and the CDN URL in
`cfd/template.html` together; a test fails if they disagree.
