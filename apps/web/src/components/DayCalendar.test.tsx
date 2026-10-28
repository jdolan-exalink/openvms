import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DayCalendar } from "./DayCalendar";

afterEach(cleanup);

const value = new Date(2026, 9, 28, 10);
const max = new Date(2026, 9, 28, 12);

describe("DayCalendar", () => {
  it("puts a red dot on the marked days only and says so to screen readers", () => {
    render(<DayCalendar value={value} max={max} marked={(d) => d.getDate() === 26 || d.getDate() === 27} onChange={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: /Elegir día/ }));
    expect(screen.getAllByTestId("recording-dot")).toHaveLength(2);
    expect(screen.getByRole("gridcell", { name: /26 de octubre de 2026, con grabaciones/ })).toBeInTheDocument();
    expect(screen.getByRole("gridcell", { name: /^25 de octubre de 2026$/ })).toBeInTheDocument();
  });

  it("draws no dots when nothing is marked", () => {
    render(<DayCalendar value={value} max={max} onChange={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: /Elegir día/ }));
    expect(screen.queryAllByTestId("recording-dot")).toHaveLength(0);
  });

  it("reports the month it shows when it opens and when it moves, so that month's marks can load", () => {
    const onMonthChange = vi.fn();
    render(<DayCalendar value={value} max={max} onMonthChange={onMonthChange} onChange={() => {}} />);
    expect(onMonthChange).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: /Elegir día/ }));
    expect(onMonthChange).toHaveBeenLastCalledWith(2026, 9);
    fireEvent.click(screen.getByRole("button", { name: "Mes anterior" }));
    expect(onMonthChange).toHaveBeenLastCalledWith(2026, 8);
  });

  it("opens above its button by default and below when asked", () => {
    const { rerender } = render(<DayCalendar value={value} max={max} onChange={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: /Elegir día/ }));
    expect(screen.getByRole("dialog").className).toContain("bottom-full");
    rerender(<DayCalendar value={value} max={max} placement="below" onChange={() => {}} />);
    expect(screen.getByRole("dialog").className).toContain("top-full");
  });

  it("still picks a day", () => {
    const onChange = vi.fn();
    render(<DayCalendar value={value} max={max} onChange={onChange} />);
    fireEvent.click(screen.getByRole("button", { name: /Elegir día/ }));
    fireEvent.click(screen.getByRole("gridcell", { name: /^20 de octubre de 2026$/ }));
    expect(onChange).toHaveBeenCalledTimes(1);
    expect((onChange.mock.calls[0]![0] as Date).getDate()).toBe(20);
  });
});
