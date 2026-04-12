#[derive(Debug, Clone, PartialEq)]
pub enum Side {
    Buy,
    Sell,
}

#[derive(Debug, Clone)]
pub struct Order {
    pub id: u64,
    pub side: Side,
    pub price: u64,      // in cents — never floats in finance
    pub quantity: u64,
    pub timestamp: u64,  // arrival time, used for time priority
}

impl Order {
    pub fn new(id: u64, side: Side, price: u64, quantity: u64, timestamp: u64) -> Self {
        Order { id, side, price, quantity, timestamp }
    }
}

#[derive(Debug, Clone)]
pub struct Fill {
    pub buy_order_id: u64,
    pub sell_order_id: u64,
    pub price: u64,
    pub quantity: u64,
}