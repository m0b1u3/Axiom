import {Agent,ProxyAgent} from "undici";
import type {ExecutionRef,ExecutionTokenUsage} from '@axiom/contracts';

/** Model turns follow SDK/task cancellation. Do not add the HTTP client's
 * default five-minute header/body timeout on either loopback proxy hop. */
export function upstreamDispatcher(upstreamBaseUrl: string): Agent | ProxyAgent {
  const options = { headersTimeout: 0, bodyTimeout: 0 };
  const proxy = process.env["HTTPS_PROXY"] ?? process.env["https_proxy"] ?? process.env["HTTP_PROXY"] ?? process.env["http_proxy"];
  if (proxy) {
    const host = new URL(upstreamBaseUrl).hostname;
    if (!["127.0.0.1", "localhost", "::1", "[::1]"].includes(host)) return new ProxyAgent({ uri: proxy, ...options });
  }
  return new Agent(options);
}

export interface UpstreamProxy {
  readonly budgetSignal?:AbortSignal;
  /** 给 Codex 用的 base_url（http://127.0.0.1:<port>） */
  readonly baseUrl: string;
  readonly upstreamBaseUrl: string;
  tokenUsage?(execution:ExecutionRef,complete:boolean):ExecutionTokenUsage|undefined;
  close(): Promise<void>;
}

