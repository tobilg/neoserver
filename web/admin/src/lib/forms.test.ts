import { describe, expect, it, vi } from "vitest";
import type { UseFormReturn } from "react-hook-form";
import { ApiError } from "@/api/client";
import { applyServerError, SERVER_ERROR_KEY } from "./forms";

interface Values {
  name: string;
  connection_info: { host: string; port: number };
}

function formStub() {
  const setError = vi.fn();
  return {
    form: { setError } as unknown as UseFormReturn<Values>,
    setError,
  };
}

describe("applyServerError", () => {
  it("attaches the error to a field the server named", () => {
    const { form, setError } = formStub();
    applyServerError(
      form,
      new ApiError(422, "Invalid store", 'field "host" is required'),
      ["name", "connection_info.host"],
    );
    expect(setError).toHaveBeenCalledWith(
      "connection_info.host",
      expect.objectContaining({ type: "server" }),
    );
  });

  it("includes both message and detail in the rendered text", () => {
    const { form, setError } = formStub();
    applyServerError(
      form,
      new ApiError(409, "Conflict", "name already exists"),
      ["name"],
    );
    expect(setError).toHaveBeenCalledWith(
      "name",
      expect.objectContaining({ message: "Conflict: name already exists" }),
    );
  });

  it("falls back to a form-level error when no field matches", () => {
    const { form, setError } = formStub();
    applyServerError(
      form,
      new ApiError(500, "Internal Error", "the datasource is unavailable"),
      ["name", "connection_info.host"],
    );
    expect(setError).toHaveBeenCalledWith(
      SERVER_ERROR_KEY,
      expect.objectContaining({ type: "server" }),
    );
  });

  it("does not match a field name inside a longer word", () => {
    const { form, setError } = formStub();
    // "invalid" contains "id"; a naive substring match would misattribute it.
    applyServerError(form, new ApiError(400, "Bad Request", "invalid body"), [
      "name",
    ]);
    expect(setError).toHaveBeenCalledWith(SERVER_ERROR_KEY, expect.anything());
  });

  it("handles non-ApiError failures", () => {
    const { form, setError } = formStub();
    applyServerError(form, new Error("network down"), ["name"]);
    expect(setError).toHaveBeenCalledWith(
      SERVER_ERROR_KEY,
      expect.objectContaining({ message: "network down" }),
    );
  });

  it("handles a thrown non-Error value", () => {
    const { form, setError } = formStub();
    applyServerError(form, "boom", ["name"]);
    expect(setError).toHaveBeenCalledWith(
      SERVER_ERROR_KEY,
      expect.objectContaining({ message: "The request failed." }),
    );
  });
});
