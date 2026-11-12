# Volcengine CLI run_script worker bootstrap (protocol version 1).
#
# The broker starts the interpreter with this file as `-c` source. Frames are
# a 4-byte big-endian length followed by a UTF-8 JSON object. The original
# stdin/stdout are duplicated into private protocol descriptors, then fd 0 is
# pointed at the null device and fd 1 at stderr, so print(), os.write(1, ...)
# and child processes cannot write into the protocol stream.
import os
import sys


def _ve_main():
    import ast
    import asyncio
    import inspect
    import itertools
    import json
    import struct
    import threading
    import traceback

    proto_in = os.dup(0)
    proto_out = os.dup(1)
    null_fd = os.open(os.devnull, os.O_RDONLY)
    os.dup2(null_fd, 0)
    os.close(null_fd)
    os.dup2(2, 1)
    if sys.platform == "win32":
        import msvcrt

        msvcrt.setmode(proto_in, os.O_BINARY)
        msvcrt.setmode(proto_out, os.O_BINARY)
    reader = os.fdopen(proto_in, "rb", buffering=0)
    writer = os.fdopen(proto_out, "wb", buffering=0)
    write_lock = threading.Lock()

    def read_exact(size):
        chunks = []
        while size > 0:
            chunk = reader.read(size)
            if not chunk:
                return None
            chunks.append(chunk)
            size -= len(chunk)
        return b"".join(chunks)

    def read_frame():
        header = read_exact(4)
        if header is None:
            return None
        (length,) = struct.unpack(">I", header)
        body = read_exact(length)
        if body is None:
            return None
        return json.loads(body.decode("utf-8"))

    def send(message):
        body = json.dumps(message, ensure_ascii=False, allow_nan=False, separators=(",", ":")).encode("utf-8")
        with write_lock:
            writer.write(struct.pack(">I", len(body)) + body)

    def exit_now(code):
        try:
            sys.stdout.flush()
            sys.stderr.flush()
        finally:
            os._exit(code)

    start = read_frame()
    if not start or start.get("type") != "start" or start.get("protocol") != 1:
        exit_now(70)

    class APIError(Exception):
        """Raised when the broker rejects or fails one call_api request."""

        def __init__(self, error, seq):
            self.code = error.get("code", "")
            self.status = error.get("status", "")
            self.request_id = error.get("request_id", "")
            self.seq = seq
            super().__init__(error.get("message", self.code))

    loop = asyncio.new_event_loop()
    asyncio.set_event_loop(loop)
    pending = {}
    ids = itertools.count(1)
    state = {"task": None}

    def deliver(message):
        future = pending.pop(message.get("id"), None)
        if future is not None and not future.done():
            future.set_result(message)

    def cancel_script():
        task = state["task"]
        if task is not None and not task.done():
            task.cancel()

    def read_loop():
        while True:
            try:
                message = read_frame()
            except Exception:
                message = None
            if message is None:
                # The broker is gone; never keep running unattended.
                exit_now(71)
            kind = message.get("type")
            if kind == "result":
                loop.call_soon_threadsafe(deliver, message)
            elif kind == "cancel":
                loop.call_soon_threadsafe(cancel_script)

    async def call_api(service, api, parameters=None, region=None):
        """Call one Volcengine OpenAPI action through the CLI broker."""
        if not isinstance(service, str) or not service:
            raise TypeError("service must be a non-empty string")
        if not isinstance(api, str) or not api:
            raise TypeError("api must be a non-empty string")
        if parameters is None:
            parameters = {}
        if not isinstance(parameters, dict):
            raise TypeError("parameters must be a dict")
        if region is not None and not isinstance(region, str):
            raise TypeError("region must be a string")
        call_id = next(ids)
        message = {"type": "call", "id": call_id, "service": service, "api": api, "parameters": parameters}
        if region:
            message["region"] = region
        future = loop.create_future()
        pending[call_id] = future
        try:
            send(message)
            response = await future
        finally:
            pending.pop(call_id, None)
        if response.get("ok"):
            return response.get("data")
        raise APIError(response.get("error") or {}, response.get("seq", 0))

    script_globals = {
        "__name__": "__ve_script__",
        "__builtins__": __builtins__,
        "args": start.get("args"),
        "call_api": call_api,
        "APIError": APIError,
    }

    def error_payload(error):
        # Keep only the user's script frames; bootstrap and asyncio frames are noise.
        frames = [frame for frame in traceback.extract_tb(error.__traceback__) if frame.filename == "<script>"]
        lines = traceback.format_list(frames) + traceback.format_exception_only(type(error), error)
        text = ("Traceback (most recent call last):\n" if frames else "") + "".join(lines)
        return {"type": type(error).__name__, "message": str(error)[:4000], "traceback": text[-8000:]}

    async def run_script():
        code = compile(start.get("code", ""), "<script>", "exec", flags=ast.PyCF_ALLOW_TOP_LEVEL_AWAIT, dont_inherit=True)
        value = eval(code, script_globals)
        if inspect.iscoroutine(value):
            await value

    threading.Thread(target=read_loop, name="ve-protocol-reader", daemon=True).start()
    state["task"] = loop.create_task(run_script())
    try:
        loop.run_until_complete(state["task"])
    except asyncio.CancelledError:
        exit_now(72)
    except SystemExit as error:
        if error.code not in (None, 0):
            send({"type": "error", "error": error_payload(error)})
            exit_now(0)
    except BaseException as error:  # noqa: B902 - report every script failure
        send({"type": "error", "error": error_payload(error)})
        exit_now(0)

    result = script_globals.get("result")
    try:
        json.dumps(result, allow_nan=False)
    except (TypeError, ValueError) as error:
        send({"type": "error", "error": {"type": "ResultError", "message": "result is not JSON-serializable: %s" % error, "traceback": ""}})
        exit_now(0)
    sys.stdout.flush()
    sys.stderr.flush()
    send({"type": "done", "result": result})
    exit_now(0)


_ve_main()
