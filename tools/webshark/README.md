# webshark in GoRex

GoRex shows packet captures (`.pcap`, `.pcapng`, `.cap`, and gzipped
ones) with webshark, which dissects them in the page with Wireshark
compiled to WebAssembly.

This folder is webshark's kiosk build, made by `tools/webshark/build.sh`
in the GoRex repository from the commit named in `COMMIT`. Files over
1 MB are kept gzipped; GoRex unpacks them as it serves them.

## Sources and licences

- webshark: <https://github.com/hepictel/webshark>, licensed under the
  GNU Affero General Public License v3.0 (`LICENSE` in this folder).
  The source of this build is that repository at the commit in `COMMIT`.
- webshark is a fork of webshark by Jakub Zawadzki
  (<https://bitbucket.org/jwzawadzki/webshark>), sponsored by QXIP
  (<https://github.com/QXIP>).
- Wiregasm, Wireshark compiled to WebAssembly:
  <https://github.com/goodtools/wiregasm>, and Wireshark itself:
  <https://www.wireshark.org>, <https://gitlab.com/wireshark/wireshark>,
  licensed under the GNU General Public License v2.
- FFmpeg.wasm, which webshark uses to play RTP audio:
  <https://github.com/ffmpegwasm/ffmpeg.wasm>.
