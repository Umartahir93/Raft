# Raft
This is my implementation of Raft consensus algorithm that I did for own learning. Please follow the link to learn more about raft consensus algorithm https://raft.github.io. And Soon, I will be developing same algorithm in Java as well.

In order to test my implementation please go in the raft folder and run "go test" command it will execute different test cases. The secnarios that my algorithm is testing includes:

Test: initial election ...
  ... Passed
Test: election after network failure ...
  ... Passed
Test: basic agreement ...
  ... Passed
Test: agreement despite follower failure ...
  ... Passed
Test: no agreement if too many followers fail ...
  ... Passed
Test: concurrent Start()s ...
  ... Passed
Test: rejoin of partitioned leader ...
  ... Passed
Test: leader backs up quickly over incorrect follower logs ...
  ... Passed 
  And many more
  
  Please note, there are race conditions within my implementation. That I yet need to solve. This was my first ever project in Golang. I tried my best to follow 
  all the best practises. In case If i am missing somethings I would love to know about them.
  
  
