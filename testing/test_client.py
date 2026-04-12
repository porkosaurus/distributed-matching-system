import asyncio
import websockets
import json

async def test():
    uri = "ws://localhost:8080/ws"
    async with websockets.connect(uri) as ws:
        # send a sell order
        await ws.send(json.dumps({
            "id": 1,
            "side": "sell", 
            "price": 10000,
            "quantity": 100
        }))
        print("sent sell order")

        # send a matching buy order
        await ws.send(json.dumps({
            "id": 2,
            "side": "buy",
            "price": 10000,
            "quantity": 100
        }))
        print("sent buy order")

        # wait for the fill
        fill = await asyncio.wait_for(ws.recv(), timeout=5.0)
        print(f"received fill: {fill}")

asyncio.run(test())