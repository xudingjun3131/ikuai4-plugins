/** API contract for GET /api/flows and SSE flow.* payloads. */

export type FlowState = "pending" | "active" | "done" | "error"

export type MessageDTO = {
  readonly method?: string
  readonly url?: string
  readonly proto?: string
  readonly status?: number
  readonly headers?: Readonly<Record<string, readonly string[]>>
  /** base64 of raw captured bytes */
  readonly body?: string
  readonly bodyDisplay?: string
  readonly truncated?: boolean
  readonly size?: number
}

export type FlowDTO = {
  readonly id: string
  readonly state: FlowState | string
  readonly startedAt: string
  readonly doneAt?: string
  readonly clientAddr?: string
  readonly scheme?: string
  readonly protocol?: string
  readonly request: MessageDTO
  readonly response: MessageDTO
  readonly error?: string
  readonly tags?: readonly string[]
  readonly highlightColor?: string
  readonly highlightLabel?: string
}

export type FlowsResponse = {
  readonly flows: readonly FlowDTO[]
}

export type FlowFilter = {
  readonly host: string
  readonly method: string
  readonly status: string
  readonly q: string
  readonly op: "and" | "or"
  readonly conds: string
}

export const emptyFlowFilter: FlowFilter = {
  host: "",
  method: "",
  status: "",
  q: "",
  op: "and",
  conds: "",
}

export type CapturePausedPayload = {
  readonly paused: boolean
}

export type SSEEventType =
  | "flow.added"
  | "flow.updated"
  | "cleared"
  | "capture_paused"
