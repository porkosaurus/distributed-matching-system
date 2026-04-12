use std::cmp::Reverse;
use std::collections::{BTreeMap, VecDeque};
use crate::order::{Order, Side};

pub struct OrderBook {
    // bids: highest price first — we wrap the key in Reverse so BTreeMap
    // sorts descending. best bid is always at the front.
    pub bids: BTreeMap<Reverse<u64>, VecDeque<Order>>,

    // asks: lowest price first — normal BTreeMap order.
    // best ask is always at the front.
    pub asks: BTreeMap<u64, VecDeque<Order>>,
}

impl OrderBook {
    pub fn new() -> Self {
        OrderBook {
            bids: BTreeMap::new(),
            asks: BTreeMap::new(),
        }
    }

    pub fn add_order(&mut self, order: Order) {
        match order.side {
            Side::Buy => {
                self.bids
                    .entry(Reverse(order.price))
                    .or_insert_with(VecDeque::new)
                    .push_back(order);
            }
            Side::Sell => {
                self.asks
                    .entry(order.price)
                    .or_insert_with(VecDeque::new)
                    .push_back(order);
            }
        }
    }

    pub fn cancel_order(&mut self, order_id: u64, side: &Side, price: u64) -> bool {
        match side {
            Side::Buy => {
                let should_remove;
                let found;

                {
                    // borrow starts and ends in this block
                    if let Some(level) = self.bids.get_mut(&Reverse(price)) {
                        let before = level.len();
                        level.retain(|o| o.id != order_id);
                        found = level.len() < before;
                        should_remove = level.is_empty();
                    } else {
                        return false;
                    }
                } // mutable borrow of self.bids ends here

                if should_remove {
                    self.bids.remove(&Reverse(price));
                }

                found
            }
            Side::Sell => {
                let should_remove;
                let found;

                {
                    if let Some(level) = self.asks.get_mut(&price) {
                        let before = level.len();
                        level.retain(|o| o.id != order_id);
                        found = level.len() < before;
                        should_remove = level.is_empty();
                    } else {
                        return false;
                    }
                } // mutable borrow of self.asks ends here

                if should_remove {
                    self.asks.remove(&price);
                }

                found
            }
        }
    }

    pub fn best_bid(&self) -> Option<u64> {
        self.bids.keys().next().map(|Reverse(p)| *p)
    }

    pub fn best_ask(&self) -> Option<u64> {
        self.asks.keys().next().copied()
    }
}