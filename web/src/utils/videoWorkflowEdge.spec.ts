import { Position } from '@vue-flow/core'
import { describe, expect, it } from 'vitest'
import { adjustableBezierGeometry, adjustableEdgeGeometry, MAX_EDGE_CURVE_OFFSET, normalizeEdgeCurve } from './videoWorkflowEdge'

describe('adjustable video workflow edge', () => {
  it('builds a cubic bezier and moves its center by the stored offset', () => {
    const base = adjustableBezierGeometry({
      sourceX: 0,
      sourceY: 20,
      sourcePosition: Position.Right,
      targetX: 240,
      targetY: 100,
      targetPosition: Position.Left,
    })
    const moved = adjustableBezierGeometry({
      sourceX: 0,
      sourceY: 20,
      sourcePosition: Position.Right,
      targetX: 240,
      targetY: 100,
      targetPosition: Position.Left,
      curve: { x: 32, y: -48 },
    })

    expect(base.path).toMatch(/^M0,20 C/)
    expect(moved.control).toEqual({ x: base.defaultCenter.x + 32, y: base.defaultCenter.y - 48 })
    expect(moved.path).not.toBe(base.path)
  })

  it('normalizes invalid values and bounds extreme offsets', () => {
    expect(normalizeEdgeCurve({ x: Number.NaN, y: Number.POSITIVE_INFINITY })).toEqual({ x: 0, y: 0 })
    expect(normalizeEdgeCurve({ x: 90_000, y: -90_000 })).toEqual({
      x: MAX_EDGE_CURVE_OFFSET,
      y: -MAX_EDGE_CURVE_OFFSET,
    })
  })

  it('rounds obstacle-avoiding route corners and applies manual offset', () => {
    const geometry = adjustableEdgeGeometry({
      sourceX: 0,
      sourceY: 50,
      sourcePosition: Position.Right,
      targetX: 300,
      targetY: 160,
      targetPosition: Position.Left,
      route: [{ x: 40, y: 50 }, { x: 40, y: 20 }, { x: 260, y: 20 }, { x: 260, y: 160 }],
      curve: { x: 0, y: 10 },
    })

    expect(geometry.path).toContain(' Q')
    expect(geometry.path).toContain('40,60')
    expect(geometry.control.y).toBe(30)
  })
})
