import type { VideoWorkflowGraph } from '@/api/videoWorkflow'
import { elkLayoutVideoWorkflowGraph } from './videoWorkflowElk'
import type { VideoWorkflowLayoutMode } from './videoWorkflowLayout'

interface LayoutRequest {
  requestID: number
  graph: VideoWorkflowGraph
  mode: VideoWorkflowLayoutMode
}

self.onmessage = async (event: MessageEvent<LayoutRequest>) => {
  try {
    const graph = await elkLayoutVideoWorkflowGraph(event.data.graph, event.data.mode)
    self.postMessage({ requestID: event.data.requestID, graph })
  } catch (error) {
    self.postMessage({ requestID: event.data.requestID, error: error instanceof Error ? error.message : String(error) })
  }
}
