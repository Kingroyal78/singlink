//go:build linux

package oomprofile

import (
	"bytes"
	"os"
	"strconv"
	"strings"
)

var (
	procMapsSpace   = []byte(" ")
	procMapsNewline = []byte("\n")
)

func (b *profileBuilder) readMapping() {
	data, _ := os.ReadFile("/proc/self/maps")
	parseProcSelfMaps(data, func(lo, hi, offset uint64, file, buildID string) {
		b.addMappingEntry(lo, hi, offset, file, buildID, false)
	})
	if len(b.mem) == 0 {
		b.addMappingEntry(0, 0, 0, "", "", true)
	}
}

func parseProcSelfMaps(data []byte, addMapping func(lo, hi, offset uint64, file, buildID string)) {
	var line []byte
	next := func() []byte {
		var field []byte
		field, line, _ = bytes.Cut(line, procMapsSpace)
		line = bytes.TrimLeft(line, " ")
		return field
	}

	for len(data) > 0 {
		line, data, _ = bytes.Cut(data, procMapsNewline)
		addr := next()
		loString, hiString, ok := strings.Cut(string(addr), "-")
		if !ok {
			continue
		}
		lo, err := strconv.ParseUint(loString, 16, 64)
		if err != nil {
			continue
		}
		hi, err := strconv.ParseUint(hiString, 16, 64)
		if err != nil {
			continue
		}
		perm := next()
		if len(perm) < 4 || perm[2] != 'x' {
			continue
		}
		offset, err := strconv.ParseUint(string(next()), 16, 64)
		if err != nil {
			continue
		}
		next()          // dev
		inode := next() // inode
		if line == nil {
			continue
		}
		file := string(line)

		const deletedSuffix = " (deleted)"
		file = strings.TrimSuffix(file, deletedSuffix)

		if len(inode) == 1 && inode[0] == '0' && file == "" {
			continue
		}

		buildID, _ := elfBuildID(file)
		addMapping(lo, hi, offset, file, buildID)
	}
}
