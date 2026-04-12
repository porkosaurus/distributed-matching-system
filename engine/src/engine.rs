use std::sync::Arc;
use std::thread;
use crossbeam::queue::SegQueue;
use crate::order::{Fill, Order};
use crate::order_book::OrderBook;
use crate::matching::match_order;

// EngineHandle is what producers hold — they use it to submit orders
// and read fills. Arc lets multiple threads share ownership safely.
pub struct EngineHandle {
    pub order_queue: Arc<SegQueue<Order>>,
    pub fill_queue:  Arc<SegQueue<Fill>>,
}

impl EngineHandle {
    pub fn submit(&self, order: Order) {
        self.order_queue.push(order);
    }

    pub fn try_get_fill(&self) -> Option<Fill> {
        self.fill_queue.pop()
    }
}

// start_engine spins up the matching thread and returns a handle
// that producers can use to talk to it.
pub fn start_engine() -> EngineHandle {
    let order_queue = Arc::new(SegQueue::<Order>::new());
    let fill_queue  = Arc::new(SegQueue::<Fill>::new());

    // clone Arcs to move into the matching thread
    let order_q = Arc::clone(&order_queue);
    let fill_q  = Arc::clone(&fill_queue);

    thread::spawn(move || {
        // this thread owns the order book exclusively
        // no other thread ever touches it — no locks needed
        let mut book = OrderBook::new();

        loop {
            // pop() returns None if the queue is empty
            // we spin (busy-wait) here — in production you'd use
            // a more sophisticated approach but this is correct
            if let Some(order) = order_q.pop() {
                let fills = match_order(&mut book, order);
                for fill in fills {
                    fill_q.push(fill);
                }
            }
        }
    });

    EngineHandle { order_queue, fill_queue }
}