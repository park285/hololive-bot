import Init

namespace JSONNameOrder

abbrev Scalar := {n : Nat // n < 0xD800 ∨ (0xE000 ≤ n ∧ n ≤ 0x10FFFF)}
abbrev Name := List Scalar

def utf16CodeUnitOrder (c : Scalar) : Nat :=
  if 0xE000 ≤ c.val ∧ c.val ≤ 0xFFFF then c.val + 0x110000 else c.val

def compareJSONNamesUTF16 : Name → Name → Ordering
  | [], [] => .eq
  | [], _ :: _ => .lt
  | _ :: _, [] => .gt
  | x :: xs, y :: ys =>
    if x ≠ y then compare (utf16CodeUnitOrder x) (utf16CodeUnitOrder y)
    else compareJSONNamesUTF16 xs ys

def prependUnits (c : Scalar) (tail : List Nat) : List Nat :=
  if c.val ≤ 0xFFFF then c.val :: tail else
    let u := c.val - 0x10000
    (0xD800 + u / 1024) :: (0xDC00 + u % 1024) :: tail

def utf16 : Name → List Nat
  | [] => []
  | c :: cs => prependUnits c (utf16 cs)

private theorem rankInjective {x y : Scalar}
    (h : utf16CodeUnitOrder x = utf16CodeUnitOrder y) : x = y := by
  have hx := x.property
  have hy := y.property
  apply Subtype.ext
  unfold utf16CodeUnitOrder at h
  split at h <;> split at h <;> omega

private theorem rankLtUnitsLt {x y : Scalar} (xs ys : List Nat)
    (h : utf16CodeUnitOrder x < utf16CodeUnitOrder y) : prependUnits x xs < prependUnits y ys := by
  have hx := x.property
  have hy := y.property
  unfold utf16CodeUnitOrder at h
  unfold prependUnits
  split at h <;> split at h <;> split <;> split <;>
    simp only [List.cons_lt_cons_iff] <;> omega

private theorem compareSamePrefix (x : Scalar) (xs ys : List Nat) :
    compare (prependUnits x xs) (prependUnits x ys) = compare xs ys := by
  unfold prependUnits
  split <;> simp

theorem agreesWithUTF16 (xs ys : Name) : compareJSONNamesUTF16 xs ys = compare (utf16 xs) (utf16 ys) := by
  induction xs generalizing ys with
  | nil =>
    cases ys with
    | nil => rfl
    | cons y ys => simp [compareJSONNamesUTF16, utf16, prependUnits]; split <;> rfl
  | cons x xs ih =>
    cases ys with
    | nil => simp [compareJSONNamesUTF16, utf16, prependUnits]; split <;> rfl
    | cons y ys =>
      simp only [compareJSONNamesUTF16, utf16]
      by_cases same : x = y
      · subst y; simpa [compareSamePrefix] using ih ys
      · simp only [same, ne_eq, not_false_eq_true, ite_true]
        rcases Nat.lt_trichotomy (utf16CodeUnitOrder x) (utf16CodeUnitOrder y) with lt | eq | gt
        · rw [Std.compare_eq_lt.mpr lt, Std.compare_eq_lt.mpr (rankLtUnitsLt _ _ lt)]
        · exact False.elim (same (rankInjective eq))
        · rw [Std.compare_eq_gt.mpr gt, Std.compare_eq_gt.mpr (rankLtUnitsLt _ _ gt)]

theorem equalExactly (xs ys : Name) : compareJSONNamesUTF16 xs ys = .eq ↔ xs = ys := by
  induction xs generalizing ys with
  | nil => cases ys <;> simp [compareJSONNamesUTF16]
  | cons x xs ih =>
    cases ys with
    | nil => simp [compareJSONNamesUTF16]
    | cons y ys =>
      by_cases same : x = y
      · subst y; simp [compareJSONNamesUTF16, ih]
      · have different : utf16CodeUnitOrder x ≠ utf16CodeUnitOrder y := fun h => same (rankInjective h)
        simp [compareJSONNamesUTF16, same, different]

def before (xs ys : Name) := compareJSONNamesUTF16 xs ys = .lt

theorem beforeIff (xs ys : Name) : before xs ys ↔ utf16 xs < utf16 ys := by
  rw [before, agreesWithUTF16, Std.compare_eq_lt]

theorem irreflexive (xs : Name) : ¬before xs xs := by simp [beforeIff]

theorem transitive {xs ys zs : Name} (xy : before xs ys) (yz : before ys zs) : before xs zs :=
  (beforeIff _ _).mpr (List.lt_trans ((beforeIff _ _).mp xy) ((beforeIff _ _).mp yz))

theorem trichotomy (xs ys : Name) : before xs ys ∨ xs = ys ∨ before ys xs := by
  cases h : compareJSONNamesUTF16 xs ys with
  | lt => exact Or.inl h
  | eq => exact Or.inr (Or.inl ((equalExactly _ _).mp h))
  | gt => rw [agreesWithUTF16, Std.compare_eq_gt] at h; exact Or.inr (Or.inr ((beforeIff _ _).mpr h))

theorem canonicalMembersUnique {left right : List (Name × α)}
    (l : left.Pairwise (fun a b => before a.1 b.1))
    (r : right.Pairwise (fun a b => before a.1 b.1)) (permutation : left.Perm right) : left = right :=
  List.Perm.eq_of_pairwise (fun _ _ _ _ ab ba => False.elim (irreflexive _ (transitive ab ba))) l r permutation

end JSONNameOrder
