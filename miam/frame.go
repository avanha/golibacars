package miam

import (
	"strings"

	"github.com/avanha/golibacars/internal/ljson"
	"github.com/avanha/golibacars/internal/proto"
)

var frameNames = map[byte]string{
	'T': "Single Transfer",
	'F': "File Transfer Request",
	'K': "File Transfer Accept",
	'S': "File Segment",
	'A': "File Transfer Abort",
	'Y': "File Transfer Pause",
	'X': "File Transfer Resume",
}

// strntouint is la_strntouint16_t: the first n characters as a decimal number, or -1 or -2.
func strntouint(s string, n int) int {
	if len(s) < n {
		return -1
	}
	v := 0
	for i := 0; i < n; i++ {
		if s[i] < '0' || s[i] > '9' {
			return -2
		}
		v = v*10 + int(s[i]-'0')
	}
	return v
}

// chompedLen is the length of s without trailing CR and LF characters.
func chompedLen(s string) int { return len(strings.TrimRight(s, "\r\n")) }

// Parse decodes a MIAM frame: the ACARS text of a message with label MA (or H1), starting at the frame identifier.
// It returns nil if the text is not a MIAM frame.
func Parse(txt string) proto.Tree {
	if i := strings.IndexByte(txt, 0); i >= 0 {
		txt = txt[:i]
	}
	if len(txt) == 0 {
		return nil
	}
	fid := txt[0]
	name, ok := frameNames[fid]
	if !ok {
		return nil
	}
	txt = txt[1:]
	var rest proto.Tree
	switch fid {
	case 'T':
		core := parseCore(txt)
		if core == nil {
			return nil
		}
		rest = append(proto.Tree{&node{key: "single_transfer"}}, core...)
	case 'F':
		n := fileTransferRequest(txt)
		if n == nil {
			return nil
		}
		rest = proto.Tree{n}
	case 'K':
		n := fileTransferAccept(txt)
		if n == nil {
			return nil
		}
		rest = proto.Tree{n}
	case 'S':
		n := fileSegment(txt)
		if n == nil {
			return nil
		}
		rest = proto.Tree{n}
		rest = append(rest, parseCore(txt[6:])...)
	case 'A':
		n := fileTransferAbort(txt)
		if n == nil {
			return nil
		}
		rest = proto.Tree{n}
	case 'Y':
		n := xoff(txt)
		if n == nil {
			return nil
		}
		rest = proto.Tree{n}
	case 'X':
		n := xon(txt)
		if n == nil {
			return nil
		}
		rest = proto.Tree{n}
	}
	top := &node{key: "miam", text: func(sb *strings.Builder, indent int) {
		printf(sb, indent, "MIAM:\n")
		printf(sb, indent+1, "%s:\n", name)
	}}
	return append(proto.Tree{top}, rest...)
}

func fileTransferRequest(txt string) proto.Node {
	if chompedLen(txt) != 21 {
		return nil
	}
	id := strntouint(txt, 3)
	if id < 0 {
		return nil
	}
	size := strntouint(txt[3:], 6)
	if size < 0 {
		return nil
	}
	t := txt[9:]
	for i := 0; i < 12; i++ {
		if t[i] < '0' || t[i] > '9' {
			return nil
		}
	}
	at := func(i int) int { return int(t[i]-'0')*10 + int(t[i+1]-'0') }
	year, mon, day, hour, min, sec := at(0)+100, at(2)-1, at(4), at(6), at(8), at(10)
	if mon > 11 || day > 31 || hour > 23 || min > 59 || sec > 59 {
		return nil
	}
	return &node{key: "file_transfer_request",
		text: func(sb *strings.Builder, indent int) {
			indent++
			printf(sb, indent, "File ID: %d\n", id)
			printf(sb, indent, "File size: %d bytes\n", size)
			printf(sb, indent, "Complete until: %d-%02d-%02d %02d:%02d:%02d\n", year+1900, mon+1, day, hour, min, sec)
			printf(sb, indent, "Reassembly: %s\n", "unknown")
		},
		json: func(w *ljson.W) {
			w.Int64("file_id", int64(id))
			w.Int64("file_size", int64(size))
			w.ObjectStart("complete_until_datetime")
			w.ObjectStart("date")
			w.Int64("year", int64(year+1900))
			w.Int64("month", int64(mon+1))
			w.Int64("day", int64(day))
			w.ObjectEnd()
			w.ObjectStart("time")
			w.Int64("hour", int64(hour))
			w.Int64("minute", int64(min))
			w.Int64("second", int64(sec))
			w.ObjectEnd()
			w.ObjectEnd()
		}}
}

func fileTransferAccept(txt string) proto.Node {
	if chompedLen(txt) != 10 {
		return nil
	}
	id := strntouint(txt, 3)
	if id < 0 {
		return nil
	}
	var seg int
	switch c := txt[3]; {
	case c >= '0' && c <= '9':
		seg = int(c - '0')
	case c >= 'A' && c <= 'F':
		seg = int(c-'A') + 10
	default:
		return nil
	}
	ground := strntouint(txt[4:], 3)
	if ground < 0 {
		return nil
	}
	flight := strntouint(txt[7:], 3)
	if flight < 0 {
		return nil
	}
	return &node{key: "file_transfer_accept",
		text: func(sb *strings.Builder, indent int) {
			indent++
			printf(sb, indent, "File ID: %d\n", id)
			printf(sb, indent, "Segment size: %d\n", seg)
			printf(sb, indent, "On-ground segment temporization: %d sec\n", ground)
			printf(sb, indent, "In-flight segment temporization: %d sec\n", flight)
		},
		json: func(w *ljson.W) {
			w.Int64("file_id", int64(id))
			w.Int64("segment_size", int64(seg))
			w.Int64("on_ground_seg_temp_secs", int64(ground))
			w.Int64("in_flight_seg_temp_secs", int64(flight))
		}}
}

func fileSegment(txt string) proto.Node {
	id := strntouint(txt, 3)
	if id < 0 {
		return nil
	}
	seg := strntouint(txt[3:], 3)
	if seg < 0 {
		return nil
	}
	return &node{key: "file_segment",
		text: func(sb *strings.Builder, indent int) {
			indent++
			printf(sb, indent, "File ID: %d\n", id)
			printf(sb, indent, "Segment ID: %d\n", seg)
			printf(sb, indent, "Reassembly: %s\n", "unknown")
		},
		json: func(w *ljson.W) {
			w.Int64("file_id", int64(id))
			w.Int64("segment_id", int64(seg))
		}}
}

var abortReasons = []string{
	"File transfer request refused by receiver",
	"File segment out of context",
	"File transfer stopped by sender",
	"File transfer stopped by receiver",
	"File segment transmission failed",
}

func fileTransferAbort(txt string) proto.Node {
	if chompedLen(txt) != 4 {
		return nil
	}
	id := strntouint(txt, 3)
	if id < 0 {
		return nil
	}
	if txt[3] < '0' || txt[3] > '9' {
		return nil
	}
	reason := int(txt[3] - '0')
	return &node{key: "file_transfer_abort",
		text: func(sb *strings.Builder, indent int) {
			indent++
			printf(sb, indent, "File ID: %d\n", id)
			descr := "unknown"
			if reason < len(abortReasons) {
				descr = abortReasons[reason]
			}
			printf(sb, indent, "Reason: %d (%s)\n", reason, descr)
		},
		json: func(w *ljson.W) {
			w.Int64("file_id", int64(id))
			w.Int64("reason", int64(reason))
		}}
}

// fileID reads a file number that may also be FFF, meaning all files.
func fileID(txt string) (int, bool) {
	id := strntouint(txt, 3)
	if id < 0 {
		if len(txt) >= 3 && txt[:3] == "FFF" {
			return 0xFFF, true
		}
		return 0, false
	}
	return id, true
}

func fileIDText(sb *strings.Builder, indent, id int) {
	if id == 0xFFF {
		printf(sb, indent, "File ID: 0xFFF (all)\n")
	} else {
		printf(sb, indent, "File ID: %d\n", id)
	}
}

func fileIDJSON(w *ljson.W, id int) {
	w.Bool("all_files", id == 0xFFF)
	if id != 0xFFF {
		w.Int64("file_id", int64(id))
	}
}

func xoff(txt string) proto.Node {
	if chompedLen(txt) != 3 {
		return nil
	}
	id, ok := fileID(txt)
	if !ok {
		return nil
	}
	return &node{key: "file_xoff_ind",
		text: func(sb *strings.Builder, indent int) { fileIDText(sb, indent+1, id) },
		json: func(w *ljson.W) { fileIDJSON(w, id) }}
}

func xon(txt string) proto.Node {
	if chompedLen(txt) != 9 {
		return nil
	}
	id, ok := fileID(txt)
	if !ok {
		return nil
	}
	ground := strntouint(txt[3:], 3)
	if ground < 0 {
		return nil
	}
	flight := strntouint(txt[6:], 3)
	if flight < 0 {
		return nil
	}
	return &node{key: "file_xon_ind",
		text: func(sb *strings.Builder, indent int) {
			indent++
			fileIDText(sb, indent, id)
			printf(sb, indent, "On-ground segment temporization: %d sec\n", ground)
			printf(sb, indent, "In-flight segment temporization: %d sec\n", flight)
		},
		json: func(w *ljson.W) {
			fileIDJSON(w, id)
			w.Int64("on_ground_seg_temp_secs", int64(ground))
			w.Int64("in_flight_seg_temp_secs", int64(flight))
		}}
}
