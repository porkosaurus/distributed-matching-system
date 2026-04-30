enum class Role{
    Follower,
    Candidate,
    Leader
};

bool canAcceptClientCommand(Role role){
    if(role == Role::Leader){
        return true;
    } else{
        return false
    }
};