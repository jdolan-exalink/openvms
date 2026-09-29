import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it } from "vitest";
import { TagInput } from "./TagInput";

function Harness({ initial = [] as string[], suggestions = ["person", "car", "dog"] }) {
  const [value, setValue] = useState(initial);
  return (
    <>
      <TagInput label="Etiquetas" value={value} onChange={setValue} suggestions={suggestions} />
      <output data-testid="value">{JSON.stringify(value)}</output>
    </>
  );
}

const value = () => screen.getByTestId("value").textContent;

describe("TagInput", () => {
  it("adds a tag on Enter, a typed comma and blur, without duplicates", () => {
    render(<Harness />);
    const input = screen.getByLabelText("Etiquetas");
    fireEvent.change(input, { target: { value: "person" } });
    fireEvent.keyDown(input, { key: "Enter" });
    fireEvent.change(input, { target: { value: "car, " } });
    fireEvent.change(input, { target: { value: "bike" } });
    fireEvent.blur(input);
    fireEvent.change(input, { target: { value: "PERSON" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(value()).toBe('["person","car","bike"]');
    expect(input).toHaveValue("");
  });

  it("removes a tag with its button and with Backspace on an empty input", () => {
    render(<Harness initial={["person", "car", "dog"]} />);
    fireEvent.click(screen.getByRole("button", { name: "Quitar person" }));
    expect(value()).toBe('["car","dog"]');
    fireEvent.keyDown(screen.getByLabelText("Etiquetas"), { key: "Backspace" });
    expect(value()).toBe('["car"]');
  });

  it("offers suggestions that are not already selected and still allows free entry", () => {
    render(<Harness initial={["person"]} />);
    const options = Array.from(document.querySelectorAll("datalist option")).map((o) => o.getAttribute("value"));
    expect(options).toEqual(["car", "dog"]);
    const input = screen.getByLabelText("Etiquetas");
    fireEvent.change(input, { target: { value: "forklift" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(value()).toBe('["person","forklift"]');
  });

  it("does not submit the surrounding form when Enter adds a tag", () => {
    let submitted = false;
    render(
      <form onSubmit={(e) => { e.preventDefault(); submitted = true; }}>
        <Harness />
      </form>,
    );
    const input = screen.getByLabelText("Etiquetas");
    fireEvent.change(input, { target: { value: "person" } });
    expect(fireEvent.keyDown(input, { key: "Enter" })).toBe(false);
    expect(submitted).toBe(false);
  });
});
