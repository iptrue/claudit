from calc import add, sub, mul

assert add(2, 3) == 5
assert add(0, 0) == 0
assert add(-4, 9) == 5
assert add(-2, -3) == -5

assert sub(10, 4) == 6
assert sub(0, 7) == -7
assert sub(-5, -2) == -3
assert sub(3, 3) == 0

assert mul(3, 4) == 12
assert mul(-2, 6) == -12
assert mul(-3, -3) == 9
assert mul(0, 99) == 0

if __name__ == "__main__":
    print("all tests passed")
