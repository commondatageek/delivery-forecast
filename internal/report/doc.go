// Package report renders internal/app results as the forecast CLI's plain
// text (and CSV) output. Every function writes to the io.Writer it is given,
// so the CLI (os.Stdout) and the web server (a buffer) produce byte-identical
// output. Pure: no os, no clock, no logging.
package report
