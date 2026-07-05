import asyncio
import websockets
import sys

async def main():
    async with websockets.connect("ws://localhost:9999/ws") as websocket:
        print("Connected. Sending 'hello' with 100ms delays...")
        for char in "hello":
            await websocket.send(char.encode('utf-8'))
            await asyncio.sleep(0.1)
        print("Done sending.")
        
        # Keep connection open for a bit
        await asyncio.sleep(2)

if __name__ == "__main__":
    asyncio.run(main())
