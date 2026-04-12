mod order;
mod order_book;
mod matching;
mod engine;

use engine::start_engine;
use std::io::{Read, Write};

// on Windows we use a named pipe instead of a Unix socket
// the path must match what the Go gateway dials
#[cfg(windows)]
fn main() {
    use std::net::TcpListener;

    // use TCP localhost instead of named pipe for simplicity
    // 127.0.0.1:9000 — only reachable from this machine
    let listener = TcpListener::bind("127.0.0.1:9000").unwrap();
    println!("Rust engine listening on 127.0.0.1:9000");

    let engine = start_engine();

    for stream in listener.incoming() {
        let stream = stream.unwrap();
        println!("Go gateway connected");
        handle_connection(stream, &engine);
    }
}

fn handle_connection(mut stream: impl Read + Write, engine: &engine::EngineHandle) {
    use order::{Order, Side};
    use std::thread;
    use std::time::Duration;

    let mut buf = [0u8; 33];

    loop {
        if let Err(_) = read_full(&mut stream, &mut buf) {
            println!("gateway disconnected");
            return;
        }

        let id        = u64::from_le_bytes(buf[0..8].try_into().unwrap());
        let side_byte = buf[8];
        let price     = u64::from_le_bytes(buf[9..17].try_into().unwrap());
        let quantity  = u64::from_le_bytes(buf[17..25].try_into().unwrap());
        let _seq_num  = u64::from_le_bytes(buf[25..33].try_into().unwrap());

        let side = if side_byte == 0 { Side::Buy } else { Side::Sell };

        println!("engine received: id={} side={} price={} qty={}", 
            id, side_byte, price, quantity);

        engine.submit(Order::new(id, side, price, quantity, id));

        thread::sleep(Duration::from_millis(50));

        let mut fill_count = 0;
        while let Some(fill) = engine.try_get_fill() {
            fill_count += 1;
            println!("fill found: buy_id={} sell_id={} price={} qty={}", 
                fill.buy_order_id, fill.sell_order_id, fill.price, fill.quantity);

            let mut fill_buf = [0u8; 32];
            fill_buf[0..8].copy_from_slice(&fill.buy_order_id.to_le_bytes());
            fill_buf[8..16].copy_from_slice(&fill.sell_order_id.to_le_bytes());
            fill_buf[16..24].copy_from_slice(&fill.price.to_le_bytes());
            fill_buf[24..32].copy_from_slice(&fill.quantity.to_le_bytes());

            match stream.write_all(&fill_buf) {
                Ok(_) => println!("fill written to socket ok"),
                Err(e) => {
                    println!("error writing fill: {}", e);
                    return;
                }
            }
        }
        println!("fills sent this round: {}", fill_count);
    }
}

fn read_full(stream: &mut impl Read, buf: &mut [u8]) -> Result<(), ()> {
    let mut total = 0;
    while total < buf.len() {
        match stream.read(&mut buf[total..]) {
            Ok(0) => return Err(()),  // connection closed
            Ok(n) => total += n,
            Err(_) => return Err(()),
        }
    }
    Ok(())
}

#[cfg(not(windows))]
fn main() {
    println!("non-Windows not configured yet");
}