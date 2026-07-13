import type { VideoWorkflowGraph } from '@/api/videoWorkflow'
import { autoLayoutVideoWorkflowGraph, type VideoWorkflowLayoutMode } from './videoWorkflowLayout'

export type VideoWorkflowLayoutEngineName = 'elk' | 'dagre'

export interface VideoWorkflowLayoutResult {
  graph: VideoWorkflowGraph
  engine: VideoWorkflowLayoutEngineName
}

interface WorkerResponse {
  requestID: number
  graph?: VideoWorkflowGraph
  error?: string
}

let layoutWorker: Worker | null = null
let requestSequence = 0
const pending = new Map<number, {
  resolve: (value: VideoWorkflowGraph) => void
  reject: (reason: Error) => void
}>()

function rejectPending(reason: Error) {
  pending.forEach(({ reject }) => reject(reason))
  pending.clear()
}

function workerInstance() {
  if (layoutWorker) return layoutWorker
  layoutWorker = new Worker(new URL('./videoWorkflowLayout.worker.ts', import.meta.url), { type: 'module' })
  layoutWorker.onmessage = (event: MessageEvent<WorkerResponse>) => {
    const request = pending.get(event.data.requestID)
    if (!request) return
    pending.delete(event.data.requestID)
    if (event.data.graph) request.resolve(event.data.graph)
    else request.reject(new Error(event.data.error || 'ELK layout failed'))
  }
  layoutWorker.onerror = () => {
    rejectPending(new Error('ELK worker failed'))
    layoutWorker?.terminate()
    layoutWorker = null
  }
  return layoutWorker
}

function elkLayout(source: VideoWorkflowGraph, mode: VideoWorkflowLayoutMode) {
  return new Promise<VideoWorkflowGraph>((resolve, reject) => {
    const requestID = ++requestSequence
    const timer = window.setTimeout(() => {
      if (!pending.delete(requestID)) return
      reject(new Error('ELK layout timed out'))
    }, 4_000)
    pending.set(requestID, {
      resolve: (graph) => { window.clearTimeout(timer); resolve(graph) },
      reject: (error) => { window.clearTimeout(timer); reject(error) },
    })
    workerInstance().postMessage({ requestID, graph: source, mode })
  })
}

export async function layoutVideoWorkflowGraph(
  source: VideoWorkflowGraph,
  mode: VideoWorkflowLayoutMode,
): Promise<VideoWorkflowLayoutResult> {
  if (import.meta.env.VITE_VIDEO_WORKFLOW_LAYOUT_ENGINE === 'dagre' || typeof Worker === 'undefined') {
    return { graph: autoLayoutVideoWorkflowGraph(source, mode), engine: 'dagre' }
  }
  try {
    return { graph: await elkLayout(source, mode), engine: 'elk' }
  } catch {
    return { graph: autoLayoutVideoWorkflowGraph(source, mode), engine: 'dagre' }
  }
}
