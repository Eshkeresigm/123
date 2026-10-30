// mkres writes a COFF resource object (.syso) with the application icon and
// manifest, so that go build embeds them into ChessVision.exe.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"image/png"
	"log"
	"os"

	"chessvision/internal/ui"
)

const manifest = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity type="win32" name="ChessVision" version="1.0.0.0"/>
  <description>ChessVision</description>
  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security><requestedPrivileges><requestedExecutionLevel level="asInvoker" uiAccess="false"/></requestedPrivileges></security>
  </trustInfo>
  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application>
      <supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
      <supportedOS Id="{1f676c76-80e1-4239-95bb-83d0f6d0da78}"/>
      <supportedOS Id="{4a2f28e3-53b9-4441-ba9c-d69d4a4a6e38}"/>
      <supportedOS Id="{35138b9a-5d96-4fbd-8e2d-a2440225f93a}"/>
    </application>
  </compatibility>
  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true/pm</dpiAware>
      <dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">PerMonitorV2,PerMonitor</dpiAwareness>
    </windowsSettings>
  </application>
</assembly>
`

type blob struct {
	typ, id uint32
	data    []byte
}

func le32(b *bytes.Buffer, v uint32) { binary.Write(b, binary.LittleEndian, v) }
func le16(b *bytes.Buffer, v uint16) { binary.Write(b, binary.LittleEndian, v) }

func dirHeader(b *bytes.Buffer, n int) {
	le32(b, 0)
	le32(b, 0)
	le16(b, 0)
	le16(b, 0)
	le16(b, 0)
	le16(b, uint16(n))
}

func main() {
	out := flag.String("o", "rsrc_windows_amd64.syso", "output")
	flag.Parse()

	var blobs []blob
	var grp bytes.Buffer
	sizes := []int{16, 24, 32, 48, 64, 128, 256}
	le16(&grp, 0)
	le16(&grp, 1)
	le16(&grp, uint16(len(sizes)))
	for i, s := range sizes {
		var pb bytes.Buffer
		if err := png.Encode(&pb, ui.RenderIcon(s)); err != nil {
			log.Fatal(err)
		}
		id := uint32(i + 1)
		blobs = append(blobs, blob{3, id, pb.Bytes()})
		wb := byte(s)
		if s >= 256 {
			wb = 0
		}
		grp.Write([]byte{wb, wb, 0, 0})
		le16(&grp, 1)
		le16(&grp, 32)
		le32(&grp, uint32(pb.Len()))
		le16(&grp, uint16(id))
	}
	blobs = append(blobs, blob{14, 1, grp.Bytes()}, blob{24, 1, []byte(manifest)})

	// layout: root dir | per-type dirs | per-id dirs | lang dirs | data entries | data
	types := []uint32{3, 14, 24}
	byType := map[uint32][]blob{}
	for _, b := range blobs {
		byType[b.typ] = append(byType[b.typ], b)
	}
	off := 16 + 8*len(types)
	typeOff := map[uint32]int{}
	for _, t := range types {
		typeOff[t] = off
		off += 16 + 8*len(byType[t])
	}
	idOff := map[[2]uint32]int{}
	for _, t := range types {
		for _, b := range byType[t] {
			idOff[[2]uint32{t, b.id}] = off
			off += 16 + 8
		}
	}
	entOff := map[[2]uint32]int{}
	for _, t := range types {
		for _, b := range byType[t] {
			entOff[[2]uint32{t, b.id}] = off
			off += 16
		}
	}
	dataOff := map[[2]uint32]int{}
	for _, t := range types {
		for _, b := range byType[t] {
			dataOff[[2]uint32{t, b.id}] = off
			off += (len(b.data) + 7) &^ 7
		}
	}
	total := off

	var sec bytes.Buffer
	dirHeader(&sec, len(types))
	for _, t := range types {
		le32(&sec, t)
		le32(&sec, 0x80000000|uint32(typeOff[t]))
	}
	for _, t := range types {
		dirHeader(&sec, len(byType[t]))
		for _, b := range byType[t] {
			le32(&sec, b.id)
			le32(&sec, 0x80000000|uint32(idOff[[2]uint32{t, b.id}]))
		}
	}
	for _, t := range types {
		for _, b := range byType[t] {
			dirHeader(&sec, 1)
			le32(&sec, 0x0409)
			le32(&sec, uint32(entOff[[2]uint32{t, b.id}]))
		}
	}
	var relocs []uint32
	for _, t := range types {
		for _, b := range byType[t] {
			relocs = append(relocs, uint32(sec.Len()))
			le32(&sec, uint32(dataOff[[2]uint32{t, b.id}])) // RVA, fixed up by the relocation
			le32(&sec, uint32(len(b.data)))
			le32(&sec, 0)
			le32(&sec, 0)
		}
	}
	for _, t := range types {
		for _, b := range byType[t] {
			sec.Write(b.data)
			for pad := (len(b.data)+7)&^7 - len(b.data); pad > 0; pad-- {
				sec.WriteByte(0)
			}
		}
	}
	if sec.Len() != total {
		log.Fatalf("layout mismatch %d != %d", sec.Len(), total)
	}

	var f bytes.Buffer
	rawPtr := 20 + 40
	relPtr := rawPtr + sec.Len()
	symPtr := relPtr + 10*len(relocs)
	le16(&f, 0x8664)
	le16(&f, 1)
	le32(&f, 0)
	le32(&f, uint32(symPtr))
	le32(&f, 1)
	le16(&f, 0)
	le16(&f, 0)
	f.Write([]byte(".rsrc\x00\x00\x00"))
	le32(&f, 0)
	le32(&f, 0)
	le32(&f, uint32(sec.Len()))
	le32(&f, uint32(rawPtr))
	le32(&f, uint32(relPtr))
	le32(&f, 0)
	le16(&f, uint16(len(relocs)))
	le16(&f, 0)
	le32(&f, 0xC0300040)
	f.Write(sec.Bytes())
	for _, r := range relocs {
		le32(&f, r)
		le32(&f, 0) // symbol index 0 = .rsrc
		le16(&f, 3) // IMAGE_REL_AMD64_ADDR32NB
	}
	f.Write([]byte(".rsrc\x00\x00\x00"))
	le32(&f, 0)
	le16(&f, 1)
	le16(&f, 0)
	f.WriteByte(3)
	f.WriteByte(0)
	le32(&f, 4) // empty string table
	if err := os.WriteFile(*out, f.Bytes(), 0o644); err != nil {
		log.Fatal(err)
	}
}
