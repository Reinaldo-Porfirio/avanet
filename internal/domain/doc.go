// Package domain holds AvaNet's core types and rules: what a measurement is,
// how it is summarized and how events such as outages are derived from it.
//
// It must not import any other AvaNet package nor perform I/O (network, disk,
// clock). Everything here is pure and covered by table-driven tests.
package domain
