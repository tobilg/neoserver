import Ajv from "ajv";
import { readFileSync } from "node:fs";

const spec = JSON.parse(
  readFileSync(new URL("../openapi.json", import.meta.url), "utf8"),
);
const ajv = new Ajv({ strict: false, allErrors: true, validateFormats: true });
ajv.addFormat(
  "date-time",
  (value: string) =>
    /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/i.test(
      value,
    ) && Number.isFinite(Date.parse(value)),
);
ajv.addFormat("email", /^[^\s@]+@[^\s@]+\.[^\s@]+$/);
ajv.addFormat("uri", (value: string) => {
  try {
    new URL(value);
    return true;
  } catch {
    return false;
  }
});
for (const format of [
  "int32",
  "int64",
  "float",
  "double",
  "byte",
  "binary",
  "password",
])
  ajv.addFormat(format, true);
const validators = new Map<string, ReturnType<typeof ajv.compile>>();

export function assertContract(
  path: string,
  method: string,
  body: unknown,
  status?: number,
) {
  const route = Object.keys(spec.paths).find((template) => {
    const parts = template.split("/");
    const actual = path.split("/");
    return (
      parts.length === actual.length &&
      parts.every(
        (part, index) => /^\{.*\}$/.test(part) || part === actual[index],
      )
    );
  });
  const operation = route && spec.paths[route][method.toLowerCase()];
  if (!operation)
    throw new Error(`Unspecified API operation: ${method} ${path}`);
  const key = `${route}:${method}:${status ?? "request"}`;
  const message = `${method} ${path} ${status ?? "request"}`;
  // Authentication middleware and generic failures share the server's Error
  // envelope, including on older operations that omit those response entries.
  const content =
    status === undefined
      ? operation.requestBody
      : (operation.responses[String(status)] ??
        operation.responses.default ??
        ([401, 403, 500, 503].includes(status)
          ? {
              content: {
                "application/json": {
                  schema: { $ref: "#/components/schemas/Error" },
                },
              },
            }
          : undefined));
  if (status !== undefined && !content)
    throw new Error(`${message}: response status is not in OpenAPI`);
  const schema = content?.content?.["application/json"]?.schema;
  if (!schema) {
    if (body !== undefined && body !== null)
      throw new Error(`${message}: unexpected JSON body`);
    return;
  }
  let validate = validators.get(key);
  if (!validate) {
    validate = ajv.compile({ ...schema, components: spec.components });
    validators.set(key, validate);
  }
  if (!validate(body))
    throw new Error(
      `${message}: ${ajv.errorsText(validate.errors, { separator: "; " })}`,
    );
}
