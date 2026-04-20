mod order;
mod order_book;
mod matching;
mod engine;
mod event_log;

use engine::start_engine;
use event_log::EventLog;
use std::io::{Read, Write};
use std::net::TcpListener;
use std::path::Path;

#[cfg(windows)]
fn main() {
    let listener = TcpListener::bind("127.0.0.1:9000").unwrap();
    println!("Rust engine listening on 127.0.0.1:9000");

    let engine_handle = start_engine();

    for stream in listener.incoming() {
        let stream = stream.unwrap();
        println!("Go gateway connected");

        // open the event log — appends to existing file on restart
        let mut log = EventLog::open(Path::new("events.log"))
            .expect("failed to open event log");

        println!("event log opened — {} records so far", log.records_written());

        handle_connection(stream, &engine_handle, &mut log);
    }
}

fn handle_connection(
    mut stream: impl Read + Write,
    engine: &engine::EngineHandle,
    log: &mut EventLog,
) {
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
        let seq_num   = u64::from_le_bytes(buf[25..33].try_into().unwrap());

        let side = if side_byte == 0 { Side::Buy } else { Side::Sell };

        // log the order before submitting to engine
        if let Err(e) = log.log_order(id, side_byte, price, quantity, seq_num) {
            println!("warning: failed to write order to event log: {}", e);
        }

        println!("engine received: id={} side={} price={} qty={} seq={}",
            id, side_byte, price, quantity, seq_num);

        engine.submit(Order::new(id, side, price, quantity, id));

        thread::sleep(Duration::from_millis(50));

        let mut fill_count = 0;
        while let Some(fill) = engine.try_get_fill() {
            fill_count += 1;

            // log the fill
            if let Err(e) = log.log_fill(
                fill.buy_order_id,
                fill.sell_order_id,
                fill.price,
                fill.quantity,
            ) {
                println!("warning: failed to write fill to event log: {}", e);
            }

            println!("fill: buy_id={} sell_id={} price={} qty={}",
                fill.buy_order_id, fill.sell_order_id, fill.price, fill.quantity);

            let mut fill_buf = [0u8; 32];
            fill_buf[0..8].copy_from_slice(&fill.buy_order_id.to_le_bytes());
            fill_buf[8..16].copy_from_slice(&fill.sell_order_id.to_le_bytes());
            fill_buf[16..24].copy_from_slice(&fill.price.to_le_bytes());
            fill_buf[24..32].copy_from_slice(&fill.quantity.to_le_bytes());

            if stream.write_all(&fill_buf).is_err() {
                return;
            }
        }
        println!("fills sent: {}", fill_count);
    }
}

fn read_full(stream: &mut impl Read, buf: &mut [u8]) -> Result<(), ()> {
    let mut total = 0;
    while total < buf.len() {
        match stream.read(&mut buf[total..]) {
            Ok(0) => return Err(()),
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