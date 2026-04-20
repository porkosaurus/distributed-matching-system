use std::fs::{File, OpenOptions};
use std::io::{self, Write};
use std::path::Path;

// EventType distinguishes the two kinds of records in the log
#[repr(u8)]
pub enum EventType {
    Order = 1,
    Fill  = 2,
}

// LogRecord is a fixed-size binary record — 42 bytes total
// type(1) + id(8) + side(1) + price(8) + quantity(8) + seq(8) + reserved(8)
// fixed size means we can seek to any record by index: offset = index * 42
#[repr(C, packed)]
pub struct LogRecord {
    pub event_type: u8,   // 1 = order, 2 = fill
    pub id:         u64,
    pub side:       u8,   // 0 = buy, 1 = sell, 255 = n/a (for fills)
    pub price:      u64,
    pub quantity:   u64,
    pub seq_num:    u64,
    pub reserved:   u64,  // future use — keeps record size round
}

pub struct EventLog {
    file: File,
    records_written: u64,
}

impl EventLog {
    /// Open or create the event log at the given path
    pub fn open(path: &Path) -> io::Result<Self> {
        let file = OpenOptions::new()
            .create(true)
            .append(true)   // never overwrite — only append
            .open(path)?;

        Ok(EventLog {
            file,
            records_written: 0,
        })
    }

    /// Write an order record to the log
    pub fn log_order(
        &mut self,
        id: u64,
        side: u8,
        price: u64,
        quantity: u64,
        seq_num: u64,
    ) -> io::Result<()> {
        let record = LogRecord {
            event_type: EventType::Order as u8,
            id,
            side,
            price,
            quantity,
            seq_num,
            reserved: 0,
        };
        self.write_record(&record)
    }

    /// Write a fill record to the log
    pub fn log_fill(
        &mut self,
        buy_order_id: u64,
        sell_order_id: u64,
        price: u64,
        quantity: u64,
    ) -> io::Result<()> {
        // for fills we store buy_id in id field and sell_id in reserved
        let record = LogRecord {
            event_type: EventType::Fill as u8,
            id:         buy_order_id,
            side:       255, // not applicable for fills
            price,
            quantity,
            seq_num:    0,
            reserved:   sell_order_id,
        };
        self.write_record(&record)
    }

    /// How many records have been written since the log was opened
    pub fn records_written(&self) -> u64 {
        self.records_written
    }

    fn write_record(&mut self, record: &LogRecord) -> io::Result<()> {
        // safety: LogRecord is repr(C, packed) so this is well-defined
        let bytes = unsafe {
            std::slice::from_raw_parts(
                record as *const LogRecord as *const u8,
                std::mem::size_of::<LogRecord>(),
            )
        };
        self.file.write_all(bytes)?;
        self.file.flush()?; // ensure it hits disk
        self.records_written += 1;
        Ok(())
    }
}