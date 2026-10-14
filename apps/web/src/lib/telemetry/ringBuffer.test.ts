import { describe, expect, it } from "vitest";
import { RingBuffer } from "./ringBuffer";

describe("RingBuffer", () => {
  it("returns items chronologically before it wraps", () => {
    const ring = new RingBuffer<number>(3);
    ring.push(1);
    ring.push(2);
    expect(ring.toArray()).toEqual([1, 2]);
    expect(ring.size).toBe(2);
  });

  it("overwrites the oldest entries after wrapping and stays chronological", () => {
    const ring = new RingBuffer<number>(3);
    for (let i = 1; i <= 5; i++) ring.push(i);
    expect(ring.toArray()).toEqual([3, 4, 5]);
    expect(ring.size).toBe(3);
  });

  it("returns a copy and can be cleared", () => {
    const ring = new RingBuffer<number>(2);
    ring.push(1);
    ring.toArray().push(99);
    expect(ring.toArray()).toEqual([1]);
    ring.clear();
    expect(ring.toArray()).toEqual([]);
  });

  it("rejects a non-positive capacity", () => {
    expect(() => new RingBuffer<number>(0)).toThrow();
  });
});
