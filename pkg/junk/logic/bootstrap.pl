:-(op(1200, xfx, :-)).
:-(op(1200, xfx, -->)).
:-(op(1200, fx, :-)).
:-(op(1105, xfy, '|')).
:-(op(1100, xfy, ;)).
:-(op(1050, xfy, ->)).
:-(op(1000, xfy, ',')).
:-(op(900, fy, \+)).
:-(op(700, xfx, =)).
:-(op(700, xfx, \=)).
:-(op(700, xfx, =..)).
:-(op(400, yfx, /)).

:- built_in(true/0).
true.

%:- built_in(fail/0).
%fail :- \+true.

:- built_in(','/2).
P, Q :- call((P, Q)).
