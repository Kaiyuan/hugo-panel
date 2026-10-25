# Third-party assets

Hugo Panel ships these browser libraries as pinned, unmodified static files so
that it can run and build without Node.js:

| Asset | Version | License | Upstream |
| --- | --- | --- | --- |
| Vue | 3.5.13 | MIT | https://github.com/vuejs/core |
| PrimeVue | 4.5.4 | MIT | https://github.com/primefaces/primevue |
| PrimeVue Themes (Aura) | 4.5.4 | MIT | https://github.com/primefaces/primevue |
| PrimeIcons | 7.0.0 | MIT | https://github.com/primefaces/primeicons |
| marked | 15.0.7 | MIT | https://github.com/markedjs/marked |
| DOMPurify | 3.2.4 | Apache-2.0 OR MPL-2.0 | https://github.com/cure53/DOMPurify |

Go module licenses are declared by their respective modules
(`github.com/BurntSushi/toml`, `github.com/mattn/go-sqlite3`,
`golang.org/x/crypto`, `gopkg.in/yaml.v3`).

Hugo itself is installed in the Docker image from the official Hugo release and
is licensed under Apache-2.0.
