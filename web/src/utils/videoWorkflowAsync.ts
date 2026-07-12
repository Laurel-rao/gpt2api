export class LatestVideoWorkflowOperation {
  private sequences = new Map<string, number>()

  begin(key: string) {
    const sequence = (this.sequences.get(key) || 0) + 1
    this.sequences.set(key, sequence)
    return sequence
  }

  invalidate(key: string) {
    this.begin(key)
  }

  isCurrent(key: string, sequence: number) {
    return this.sequences.get(key) === sequence
  }
}

export class SerialVideoWorkflowOperationQueue {
  private queues = new Map<string, Promise<unknown>>()

  enqueue<T>(key: string, operation: () => Promise<T>): Promise<T> {
    const previous = this.queues.get(key) || Promise.resolve()
    const queued = previous.catch(() => undefined).then(operation)
    const tracked = queued.finally(() => {
      if (this.queues.get(key) === tracked) this.queues.delete(key)
    })
    this.queues.set(key, tracked)
    return tracked
  }
}

export async function runConfirmedVideoWorkflowMutation<T>(operations: {
  confirm: () => Promise<void>
  apply: () => Promise<void>
  submit: () => Promise<T>
  rollback: () => Promise<void>
}): Promise<T> {
  await operations.confirm()
  let mutationStarted = false
  try {
    mutationStarted = true
    await operations.apply()
    return await operations.submit()
  } catch (error) {
    if (mutationStarted) {
      try { await operations.rollback() } catch { /* 保留原始运行错误，由本地草稿继续保护恢复状态。 */ }
    }
    throw error
  }
}
