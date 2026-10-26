/** Incremental SSE framing: LF/CRLF/CR, split UTF-8, multiline data and final tail. */
export class SseParser {
  private buffer = "";
  private data: string[] = [];
  private event = "message";
  constructor(private readonly emit: (event: string, payload: string) => void) {}
  push(text: string): void {
    this.buffer += text;
    if (this.buffer.length > 8 * 1024 * 1024) throw new Error("SSE frame too large");
    for (;;) {
      const i = this.buffer.search(/[\r\n]/);
      if (i < 0 || (this.buffer[i] === "\r" && i === this.buffer.length - 1)) return;
      const line = this.buffer.slice(0, i);
      const width = this.buffer[i] === "\r" && this.buffer[i + 1] === "\n" ? 2 : 1;
      this.buffer = this.buffer.slice(i + width);
      this.line(line);
    }
  }
  end(): void {
    if (this.buffer) this.line(this.buffer.replace(/\r$/, ""));
    this.buffer = "";
    this.line("");
  }
  private line(line: string): void {
    if (!line) {
      if (this.data.length || this.event === "error") this.emit(this.event, this.data.join("\n"));
      this.data = []; this.event = "message"; return;
    }
    if (line.startsWith(":")) return;
    const colon = line.indexOf(":");
    const field = colon < 0 ? line : line.slice(0, colon);
    const value = colon < 0 ? "" : line.slice(colon + 1).replace(/^ /, "");
    if (field === "data") this.data.push(value);
    else if (field === "event") this.event = value;
  }
}

